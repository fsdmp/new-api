package exceltools

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

// lockedWriter serializes writes to the real client response writer. Loop
// phases are sequential, but synthesized blocks, pings and pass-through events
// all funnel through here so concurrent access can never corrupt the stream.
type lockedWriter struct {
	mu sync.Mutex
	w  gin.ResponseWriter
}

func (l *lockedWriter) write(p []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(p)
	if f, ok := l.w.(http.Flusher); ok {
		f.Flush()
	}
}

// emitSSE frames one SSE event and writes it.
func (l *lockedWriter) emitSSE(event string, data []byte) {
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	l.write(buf.Bytes())
}

func (l *lockedWriter) emitPing() {
	l.emitSSE("ping", []byte(`{"type":"ping"}`))
}

// emitError writes an Anthropic-style SSE error event.
func (l *lockedWriter) emitError(errType, message string) {
	payload := map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		encoded = []byte(`{"type":"error","error":{"type":"api_error","message":"internal error"}}`)
	}
	l.emitSSE("error", encoded)
}

// interceptorMode distinguishes raw pass-through (non-SSE response) from
// stream rewriting.
type interceptorMode int

const (
	modeUndecided interceptorMode = iota
	modePassthrough
	modeSSE
)

// StreamInterceptor wraps the client response writer during one Relay
// invocation. It suppresses per-iteration message framing, captures virtual
// tool_use blocks, rewrites content block indices with a global offset, and
// forwards everything else.
type StreamInterceptor struct {
	gin.ResponseWriter // delegates every method to the real writer

	real *lockedWriter
	// virtualNames is the set of virtual tool names to intercept.
	virtualNames map[string]bool
	// offset is added to every content block index before forwarding.
	offset int
	// first marks the first iteration: its message_start is forwarded with a
	// zeroed usage (later iterations' message framing is swallowed).
	first bool

	mode         interceptorMode
	buf          bytes.Buffer
	pendingEvent string
	pendingData  strings.Builder

	// suppression state
	suppressIndex   int // -1 = not suppressing
	suppressedCall  CapturedCall
	suppressArgsBuf strings.Builder

	// text capture state (for building next iteration body)
	capturingText bool
	textBuf       strings.Builder

	result IterationResult
	done   bool
}

func newStreamInterceptor(real *lockedWriter, virtualNames map[string]bool, offset int, first bool) *StreamInterceptor {
	return &StreamInterceptor{
		ResponseWriter: real.w,
		real:           real,
		virtualNames:   virtualNames,
		offset:         offset,
		first:          first,
		suppressIndex:  -1,
	}
}

// Write implements gin.ResponseWriter.
func (i *StreamInterceptor) Write(p []byte) (int, error) {
	i.feed(p)
	return len(p), nil
}

// WriteString implements gin.ResponseWriter.
func (i *StreamInterceptor) WriteString(s string) (int, error) {
	i.feed([]byte(s))
	return len(s), nil
}

// feed buffers bytes and processes complete lines. The Claude SSE writer
// emits events as "event: X\n" then "data: {...}\n\n" in separate writes, so
// framing is reconstructed from the line stream.
func (i *StreamInterceptor) feed(p []byte) {
	if i.mode == modeUndecided {
		if isEventStream(i.Header().Get("Content-Type")) {
			i.mode = modeSSE
		} else {
			i.mode = modePassthrough
		}
	}
	if i.mode == modePassthrough {
		i.real.write(p)
		if !i.result.Fatal {
			i.result.Fatal = true
		}
		return
	}

	i.buf.Write(p)
	for {
		s := i.buf.String()
		pos := strings.IndexByte(s, '\n')
		if pos < 0 {
			return // incomplete line; wait for more bytes
		}
		line := strings.TrimRight(s[:pos], "\r")
		i.buf.Reset()
		i.buf.WriteString(s[pos+1:])
		i.handleLine(line)
	}
}

func isEventStream(contentType string) bool {
	return strings.HasPrefix(contentType, "text/event-stream")
}

// handleLine implements the SSE line protocol: "event:", "data:" and blank
// line dispatch.
func (i *StreamInterceptor) handleLine(line string) {
	switch {
	case line == "":
		if i.pendingEvent != "" || i.pendingData.Len() > 0 {
			i.processEvent(i.pendingEvent, i.pendingData.String())
		}
		i.pendingEvent = ""
		i.pendingData.Reset()
	case strings.HasPrefix(line, "event:"):
		i.pendingEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	case strings.HasPrefix(line, "data:"):
		i.pendingData.WriteString(strings.TrimPrefix(line, "data:"))
	}
}

// finish flushes whatever the relay wrote without a trailing newline. This is
// where a bare JSON error body (no SSE framing, no final newline) written into
// an event-stream response gets detected and translated.
func (i *StreamInterceptor) finish() {
	if i.mode != modeSSE {
		return
	}
	if s := strings.TrimSpace(i.buf.String()); s != "" {
		i.buf.Reset()
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			// raw JSON body leaked into the SSE stream — a relay error that
			// never adopted event framing
			i.real.emitError("api_error", bestEffortErrorMessage(s))
			i.result.StreamError = true
			i.result.StreamStarted = true
			return
		}
		i.handleLine(s)
	}
	if i.pendingEvent != "" || i.pendingData.Len() > 0 {
		i.processEvent(i.pendingEvent, i.pendingData.String())
		i.pendingEvent = ""
		i.pendingData.Reset()
	}
}

// processEvent applies the rewriting rules to one parsed SSE event.
func (i *StreamInterceptor) processEvent(event, data string) {
	if event == "error" {
		i.real.emitSSE(event, []byte(data))
		i.result.StreamError = true
		i.result.StreamStarted = true
		return
	}

	var payload map[string]interface{}
	if err := common.Unmarshal([]byte(data), &payload); err != nil {
		// Non-SSE JSON in stream mode: the relay wrote a JSON error after the
		// stream had started (e.g. pre-consume failure on iteration >= 2).
		// Translate to an in-stream error event.
		i.real.emitError("api_error", bestEffortErrorMessage(data))
		i.result.StreamError = true
		i.result.StreamStarted = true
		return
	}

	i.result.StreamStarted = true

	switch event {
	case "message_start":
		i.handleMessageStart(payload)
	case "ping":
		i.real.emitSSE(event, []byte(data))
	case "content_block_start":
		i.handleBlockStart(payload)
	case "content_block_delta":
		i.handleBlockDelta(payload)
	case "content_block_stop":
		i.handleBlockStop(payload)
	case "message_delta":
		i.handleMessageDelta(payload)
	case "message_stop":
		// swallowed; the orchestrator synthesizes the closing frames
	default:
		// unknown events pass through untouched (future protocol additions)
		i.real.emitSSE(event, []byte(data))
	}
}

func (i *StreamInterceptor) handleMessageStart(payload map[string]interface{}) {
	msg, _ := payload["message"].(map[string]interface{})
	if msg != nil {
		if id, ok := msg["id"].(string); ok {
			i.result.MessageID = id
		}
		if m, ok := msg["model"].(string); ok {
			i.result.Model = m
		}
		if usage, ok := msg["usage"].(map[string]interface{}); ok {
			if n, ok := jsonNumber(usage["input_tokens"]); ok {
				i.result.Usage.InputTokens = n
			}
			if n, ok := jsonNumber(usage["cache_read_input_tokens"]); ok {
				i.result.Usage.CacheReadInputTokens = n
			}
			if n, ok := jsonNumber(usage["cache_creation_input_tokens"]); ok {
				i.result.Usage.CacheCreationInputTokens = n
			}
		}
	}
	if i.first {
		// Forward with zeroed usage; the aggregated totals arrive in the
		// final synthesized message_delta.
		if msg != nil {
			msg["usage"] = map[string]interface{}{
				"input_tokens":  0,
				"output_tokens": 0,
			}
		}
		i.reemit("message_start", payload)
	}
}

func (i *StreamInterceptor) handleBlockStart(payload map[string]interface{}) {
	block, _ := payload["content_block"].(map[string]interface{})
	blockType, _ := block["type"].(string)
	if blockType == "tool_use" {
		name, _ := block["name"].(string)
		if i.virtualNames[name] {
			// suppress and start capturing the virtual call
			idx, _ := jsonNumber(payload["index"])
			i.suppressIndex = idx
			i.suppressedCall = CapturedCall{}
			i.suppressedCall.ID, _ = block["id"].(string)
			i.suppressedCall.Name = name
			i.suppressArgsBuf.Reset()
			return
		}
		i.result.HasClientToolUse = true
	}
	if blockType == "text" {
		i.capturingText = true
		i.textBuf.Reset()
	}
	i.result.BlockCount++
	i.reemitIndexed("content_block_start", payload)
}

func (i *StreamInterceptor) handleBlockDelta(payload map[string]interface{}) {
	idx, _ := jsonNumber(payload["index"])
	if i.suppressIndex == idx {
		delta, _ := payload["delta"].(map[string]interface{})
		if dt, _ := delta["type"].(string); dt == "input_json_delta" {
			if pj, ok := delta["partial_json"].(string); ok {
				i.suppressArgsBuf.WriteString(pj)
			}
		}
		return
	}
	if i.capturingText {
		delta, _ := payload["delta"].(map[string]interface{})
		if dt, _ := delta["type"].(string); dt == "text_delta" {
			if t, ok := delta["text"].(string); ok {
				i.textBuf.WriteString(t)
			}
		}
	}
	i.reemitIndexed("content_block_delta", payload)
}

func (i *StreamInterceptor) handleBlockStop(payload map[string]interface{}) {
	idx, _ := jsonNumber(payload["index"])
	if i.suppressIndex == idx {
		call := i.suppressedCall
		raw := i.suppressArgsBuf.String()
		if raw != "" {
			var input map[string]interface{}
			if err := common.Unmarshal([]byte(raw), &input); err == nil {
				call.Input = input
			} else {
				call.RawInput = raw
			}
		}
		i.result.VirtualCalls = append(i.result.VirtualCalls, &call)
		i.suppressIndex = -1
		return
	}
	if i.capturingText {
		i.result.TextBlocks = append(i.result.TextBlocks, CapturedText{Text: i.textBuf.String()})
		i.capturingText = false
	}
	i.reemitIndexed("content_block_stop", payload)
}

func (i *StreamInterceptor) handleMessageDelta(payload map[string]interface{}) {
	if delta, ok := payload["delta"].(map[string]interface{}); ok {
		if sr, ok := delta["stop_reason"].(string); ok && sr != "" {
			i.result.StopReason = sr
		}
	}
	if usage, ok := payload["usage"].(map[string]interface{}); ok {
		if n, ok := jsonNumber(usage["output_tokens"]); ok && n > i.result.Usage.OutputTokens {
			i.result.Usage.OutputTokens = n
		}
		if n, ok := jsonNumber(usage["input_tokens"]); ok && n > i.result.Usage.InputTokens {
			i.result.Usage.InputTokens = n
		}
	}
}

// reemitIndexed rewrites the block index with the global offset and forwards.
func (i *StreamInterceptor) reemitIndexed(event string, payload map[string]interface{}) {
	if idx, ok := jsonNumber(payload["index"]); ok {
		payload["index"] = idx + i.offset
	}
	i.reemit(event, payload)
}

func (i *StreamInterceptor) reemit(event string, payload map[string]interface{}) {
	encoded, err := common.Marshal(payload)
	if err != nil {
		return
	}
	i.real.emitSSE(event, encoded)
}

// bestEffortErrorMessage extracts a message from a relay JSON error body.
func bestEffortErrorMessage(data string) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal([]byte(data), &payload); err == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	if len(data) > 300 {
		data = data[:300] + "…"
	}
	return fmt.Sprintf("upstream error: %s", data)
}
