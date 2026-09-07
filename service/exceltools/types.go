// Package exceltools implements the gateway-internal server-tool loop for the
// Excel endpoint: SSE interception, tool execution (web_search / web_fetch /
// code_execution) and result re-injection, presenting the whole exchange to
// the client as one standard Anthropic Messages SSE stream.
package exceltools

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// CapturedCall is one virtual-tool invocation emitted by the model in an
// iteration, reconstructed from the suppressed tool_use content block.
type CapturedCall struct {
	ID    string
	Name  string
	Input map[string]interface{}
	// RawInput keeps the raw JSON string when the accumulated partial_json
	// fragments do not parse (malformed model output).
	RawInput string
}

// Param returns a string parameter from the call input.
func (c *CapturedCall) Param(name string) (string, bool) {
	if c.Input == nil {
		return "", false
	}
	v, ok := c.Input[name].(string)
	return v, ok
}

// ParamStringSlice returns a []string parameter (JSON array of strings).
func (c *CapturedCall) ParamStringSlice(name string) []string {
	if c.Input == nil {
		return nil
	}
	raw, ok := c.Input[name].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// CapturedText is a pass-through text block of one iteration, kept for
// building the next iteration's request body.
type CapturedText struct {
	Text string
}

// UsageAgg accumulates token usage across loop iterations.
type UsageAgg struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u *UsageAgg) add(o UsageAgg) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.CacheCreationInputTokens += o.CacheCreationInputTokens
	u.CacheReadInputTokens += o.CacheReadInputTokens
}

// IterationResult is what the interceptor learned from one Relay invocation.
type IterationResult struct {
	// Fatal: the relay failed before any SSE event was emitted; the JSON
	// error response was passed through untouched and the loop must stop.
	Fatal bool
	// StreamError: an error occurred after the client stream had started and
	// was translated into an SSE `error` event; the loop must stop.
	StreamError bool
	// StreamStarted: at least one SSE event was forwarded to the client.
	StreamStarted bool

	VirtualCalls    []*CapturedCall
	HasClientToolUse bool
	TextBlocks      []CapturedText
	// BlockCount is the number of content blocks forwarded this iteration
	// (used to advance the global index offset).
	BlockCount int

	Usage      UsageAgg
	StopReason string
	MessageID  string
	Model      string
}

// ToolError is an executor failure that is fed back to the model as the
// tool_result text (never surfaced as an HTTP error).
type ToolError struct {
	Code    string // "quota_denied" | "max_uses" | "exec_error" | "timeout" | "invalid_input"
	Message string
}

// ExecResult is the outcome of one virtual tool execution.
type ExecResult struct {
	// Block is the content of the client-facing *_tool_result block (shape
	// isolated per provider; render mismatch degrades to no status line only).
	Block interface{}
	// ModelText is the compact text fed back to the model.
	ModelText string
	Err       *ToolError
	// StatusShort is a compact status for logs (ok / quota_denied / ...).
	StatusShort string
	// DurationMs is the wall-clock execution time.
	DurationMs int64
}

// Executor executes one virtual server tool.
type Executor interface {
	Name() string
	// Execute runs the tool. Never returns a Go error for tool-level
	// failures — those are encoded in ExecResult.Err so the loop can feed
	// them back to the model.
	Execute(sess *Session, call *CapturedCall) *ExecResult
}

// Session carries per-request execution context.
type Session struct {
	RequestID  string
	UserID     int
	Model      string // routed model name (provider resolution, billing)
	MaxUsages  map[string]int
	// MaxContentTokens maps tool name -> the request-declared
	// max_content_tokens (web_fetch); 0/absent = default budget.
	MaxContentTokens map[string]int
	usedCount        map[string]int
	Quota            *QuotaTracker
	ToolCounts       map[string]int // per-tool successful executions, for billing
}

func (s *Session) countUse(name string) int {
	if s.usedCount == nil {
		s.usedCount = map[string]int{}
	}
	s.usedCount[name]++
	return s.usedCount[name]
}

// withinMaxUses checks the request-declared max_uses for a tool.
func (s *Session) withinMaxUses(name string) bool {
	if s.MaxUsages == nil {
		return true
	}
	max, ok := s.MaxUsages[name]
	if !ok || max <= 0 {
		return true
	}
	return s.usedCount[name] < max
}

// lookupExecutor resolves the executor registered for a virtual tool name.
var executors = map[string]Executor{}

func RegisterExecutor(e Executor) {
	executors[e.Name()] = e
}

func LookupExecutor(name string) Executor {
	return executors[name]
}

// BlockForCall returns the synthesized client-facing block payload for a
// server_tool_use event of the given tool (per Anthropic docs shape).
func BlockForCall(call *CapturedCall) map[string]interface{} {
	input := call.Input
	if input == nil {
		input = map[string]interface{}{}
	}
	return map[string]interface{}{
		"type":  "server_tool_use",
		"id":    call.ID,
		"name":  call.Name,
		"input": input,
	}
}

// BlockForResult returns the synthesized *_tool_result block payload.
func BlockForResult(call *CapturedCall, content interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":       excel_tools.ResultBlockType(call.Name),
		"tool_use_id": call.ID,
		"content":     content,
	}
}

// jsonNumber is a helper for reading numeric fields from unmarshalled maps.
func jsonNumber(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case int:
		return n, true
	}
	return 0, false
}
