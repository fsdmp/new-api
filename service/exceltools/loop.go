package exceltools

import (
	"fmt"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_tools"

	"github.com/gin-gonic/gin"
)

// LoopPlan decides whether the tool loop activates for a request and carries
// its parameters.
type LoopPlan struct {
	Active        bool
	VirtualNames  map[string]bool
	MaxUsages     map[string]int
	MaxIterations int
	PingInterval  time.Duration
}

// BuildLoopPlan inspects the sanitized body and the adapter's virtual-tool
// stash. The loop only activates in full mode with at least one injected
// virtual tool on a streaming request.
func BuildLoopPlan(c *gin.Context) *LoopPlan {
	plan := &LoopPlan{}
	if excel_tools.GetMode() != excel_tools.ModeFull {
		return plan
	}
	stash, ok := c.Get(excel_tools.GinKeyVirtualTools)
	if !ok {
		return plan
	}
	info, ok := stash.(*excel_tools.VirtualToolInfo)
	if !ok || len(info.Names) == 0 {
		return plan
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return plan
	}
	data, err := storage.Bytes()
	if err != nil {
		return plan
	}
	var body map[string]interface{}
	if err := common.Unmarshal(data, &body); err != nil {
		return plan
	}
	// The loop rewrites SSE; non-streaming requests take the zero-cost path.
	if stream, ok := body["stream"].(bool); ok && !stream {
		return plan
	}

	loopCfg := excel_tools.GetLoopConfig()
	plan.Active = true
	plan.VirtualNames = make(map[string]bool, len(info.Names))
	for _, n := range info.Names {
		plan.VirtualNames[n] = true
	}
	plan.MaxUsages = info.MaxUsages
	plan.MaxIterations = loopCfg.MaxIterations
	plan.PingInterval = time.Duration(loopCfg.PingIntervalSec) * time.Second
	return plan
}

// SessionFromContext builds the per-request execution session.
func SessionFromContext(c *gin.Context) *Session {
	sess := &Session{
		RequestID:        c.GetString(common.RequestIdKey),
		UserID:           c.GetInt("id"),
		MaxUsages:        map[string]int{},
		MaxContentTokens: map[string]int{},
		ToolCounts:       map[string]int{},
	}
	if stash, ok := c.Get(excel_tools.GinKeyVirtualTools); ok {
		if info, ok := stash.(*excel_tools.VirtualToolInfo); ok {
			sess.MaxUsages = info.MaxUsages
			sess.MaxContentTokens = info.MaxContentTokens
		}
	}
	sess.Quota = NewQuotaTracker()
	return sess
}

// RunToolLoop orchestrates the iterations. Each iteration re-enters the full
// relay pipeline through `relay` (unchanged controller.Relay in production),
// with the writer swapped for a StreamInterceptor. relayFunc is injected so
// tests can drive the loop without a live upstream.
func RunToolLoop(c *gin.Context, plan *LoopPlan, relayFunc func(*gin.Context)) {
	originalWriter := c.Writer
	defer func() {
		c.Writer = originalWriter
	}()

	lw := &lockedWriter{w: originalWriter}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		relayFunc(c) // cannot loop without a body; degrade to plain relay
		return
	}
	data, err := storage.Bytes()
	if err != nil {
		relayFunc(c)
		return
	}
	var body map[string]interface{}
	if err := common.Unmarshal(data, &body); err != nil {
		relayFunc(c)
		return
	}

	sess := SessionFromContext(c)
	if m, ok := body["model"].(string); ok {
		sess.Model = m
	}

	offset := 0
	var aggUsage UsageAgg
	messageID := ""
	stopReason := "end_turn"

	for iter := 1; iter <= plan.MaxIterations; iter++ {
		newData, err := common.Marshal(body)
		if err != nil {
			lw.emitError("api_error", "failed to build iteration request")
			return
		}
		newStorage, err := common.CreateBodyStorage(newData)
		if err != nil {
			lw.emitError("api_error", "failed to build iteration request")
			return
		}
		c.Set(common.KeyBodyStorage, newStorage)
		c.Request.Body = io.NopCloser(newStorage)

		interceptor := newStreamInterceptor(lw, plan.VirtualNames, offset, iter == 1)
		c.Writer = interceptor
		relayFunc(c)
		c.Writer = originalWriter
		interceptor.finish()

		res := &interceptor.result
		if res.Fatal || res.StreamError {
			// client already received the (translated) error; nothing to close
			return
		}
		if !res.StreamStarted {
			lw.emitError("api_error", "empty response from upstream")
			return
		}

		if res.MessageID != "" {
			messageID = res.MessageID
		}
		if res.Model != "" {
			sess.Model = res.Model
		}
		if res.StopReason != "" {
			stopReason = res.StopReason
		}
		aggUsage.add(res.Usage)
		_ = messageID // message id kept for future per-iteration billing labels

		pending := res.VirtualCalls
		terminal := len(pending) == 0 || res.HasClientToolUse || iter == plan.MaxIterations

		var results []*ExecResult
		if len(pending) > 0 {
			switch {
			case res.HasClientToolUse:
				// Client tool ends the conversation: still execute the virtual
				// calls and emit blocks (client sees results), but nothing is
				// fed back — the client's next turn carries the history.
				results = executeWithKeepalive(c, lw, sess, pending, plan.PingInterval)
			case iter == plan.MaxIterations:
				// Stubborn case: model still calls tools on its final allowed
				// turn. Reject visibly and force an end_turn close.
				results = rejectResults(pending)
			case iter == plan.MaxIterations-1:
				// Budget rejection turn: feed rejection texts so the next
				// (final) turn answers directly instead of calling again.
				results = rejectResults(pending)
			default:
				results = executeWithKeepalive(c, lw, sess, pending, plan.PingInterval)
			}
			emitToolBlocks(lw, offset+res.BlockCount, pending, results)
		}

		if terminal {
			finalStop := stopReason
			if iter == plan.MaxIterations && len(pending) > 0 && !res.HasClientToolUse {
				finalStop = "end_turn"
			}
			emitFinal(lw, finalStop, aggUsage)
			PostToolBilling(c, sess)
			return
		}

		body = appendIterationBody(body, res, pending, results)
		offset += res.BlockCount + 2*len(pending)
	}

	// unreachable: the iter == MaxIterations branch always terminates
}

// rejectResults produces refusals for every pending call without executing.
func rejectResults(calls []*CapturedCall) []*ExecResult {
	results := make([]*ExecResult, 0, len(calls))
	for range calls {
		results = append(results, &ExecResult{
			ModelText:   "本回合该工具调用额度已用完，请基于已有信息直接回答。",
			StatusShort: "budget_rejected",
		})
	}
	return results
}

// executeWithKeepalive runs the executors while emitting SSE pings so the
// client (and any reverse proxy) sees the connection is alive.
func executeWithKeepalive(c *gin.Context, lw *lockedWriter, sess *Session, calls []*CapturedCall, interval time.Duration) []*ExecResult {
	done := make(chan struct{})
	var results []*ExecResult
	go func() {
		defer close(done)
		results = executeCalls(c, sess, calls)
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	clientGone := false
	for {
		select {
		case <-done:
			return results
		case <-ticker.C:
			if !clientGone && c.Request != nil && c.Request.Context().Err() != nil {
				clientGone = true // stop pinging a dead connection, keep waiting for executors
				continue
			}
			if !clientGone {
				lw.emitPing()
			}
		}
	}
}

// executeCalls runs each captured call through its executor (quota / max_uses
// checks inside); refusals come back as text results, never as errors.
func executeCalls(c *gin.Context, sess *Session, calls []*CapturedCall) []*ExecResult {
	results := make([]*ExecResult, 0, len(calls))
	for _, call := range calls {
		results = append(results, executeOne(c, sess, call))
	}
	return results
}

func executeOne(c *gin.Context, sess *Session, call *CapturedCall) *ExecResult {
	start := time.Now()

	if !sess.withinMaxUses(call.Name) {
		return &ExecResult{
			ModelText:   "本回合该工具调用次数已达上限，请基于已有信息直接回答。",
			StatusShort: "max_uses",
		}
	}
	sess.countUse(call.Name)

	executor := LookupExecutor(call.Name)
	if executor == nil {
		return &ExecResult{
			ModelText: "工具不可用：" + call.Name,
			Err:       &ToolError{Code: "exec_error", Message: "executor not registered: " + call.Name},
		}
	}

	if denied := sess.Quota.CheckDailyQuota(c, sess.UserID, call.Name); denied {
		return &ExecResult{
			ModelText:   "今日该工具使用额度已用完，请明天再试或基于已有信息回答。",
			StatusShort: "quota_denied",
		}
	}

	res := executor.Execute(sess, call)
	if res == nil {
		res = &ExecResult{Err: &ToolError{Code: "exec_error", Message: "executor returned nil"}}
	}
	if res.DurationMs == 0 {
		res.DurationMs = time.Since(start).Milliseconds()
	}
	if res.StatusShort == "" {
		if res.Err != nil {
			res.StatusShort = res.Err.Code
		} else {
			res.StatusShort = "ok"
		}
	}
	if res.Err == nil {
		sess.ToolCounts[call.Name]++
	}

	logToolCall(c, sess, call, res)
	return res
}

func logToolCall(c *gin.Context, sess *Session, call *CapturedCall, res *ExecResult) {
	inputPreview := ""
	switch call.Name {
	case "code_execution":
		if code, ok := call.Param("code"); ok {
			if len(code) > 200 {
				code = code[:200] + "…"
			}
			inputPreview = code
		}
	default:
		if q, ok := call.Param("query"); ok {
			inputPreview = q
		} else if u, ok := call.Param("url"); ok {
			inputPreview = u
		}
	}
	common.SysLog(fmt.Sprintf("[excel-tools] request_id=%s user=%d tool=%s status=%s dur_ms=%d input=%s",
		sess.RequestID, sess.UserID, call.Name, res.StatusShort, res.DurationMs, inputPreview))
}

// appendIterationBody extends the request body with the previous iteration's
// assistant output (text + virtual tool_use) and the model-facing tool_result
// texts, so the next relay iteration continues the same logical turn.
func appendIterationBody(body map[string]interface{}, res *IterationResult, calls []*CapturedCall, results []*ExecResult) map[string]interface{} {
	msgs, _ := body["messages"].([]interface{})

	assistantContent := make([]interface{}, 0, len(res.TextBlocks)+len(calls))
	for _, tb := range res.TextBlocks {
		if tb.Text != "" {
			assistantContent = append(assistantContent, map[string]interface{}{
				"type": "text", "text": tb.Text,
			})
		}
	}
	for _, call := range calls {
		input := call.Input
		if input == nil {
			input = map[string]interface{}{}
		}
		assistantContent = append(assistantContent, map[string]interface{}{
			"type": "tool_use", "id": call.ID, "name": call.Name, "input": input,
		})
	}
	if len(assistantContent) > 0 {
		msgs = append(msgs, map[string]interface{}{
			"role": "assistant", "content": assistantContent,
		})
	}

	userContent := make([]interface{}, 0, len(calls))
	for i, call := range calls {
		text := ""
		if i < len(results) && results[i] != nil {
			if results[i].Err != nil {
				text = "工具执行失败：" + results[i].Err.Message
			} else {
				text = results[i].ModelText
			}
		}
		userContent = append(userContent, map[string]interface{}{
			"type": "tool_result", "tool_use_id": call.ID, "content": text,
		})
	}
	if len(userContent) > 0 {
		msgs = append(msgs, map[string]interface{}{
			"role": "user", "content": userContent,
		})
	}

	body["messages"] = msgs
	return body
}
