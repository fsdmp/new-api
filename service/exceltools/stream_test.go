package exceltools

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_tools"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// ---------- test writer (full gin.ResponseWriter over a recorder) ----------

type captureWriter struct {
	*httptest.ResponseRecorder
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{ResponseRecorder: httptest.NewRecorder()}
}

func (w *captureWriter) WriteString(s string) (int, error) { return w.Body.WriteString(s) }
func (w *captureWriter) Status() int                      { return w.Code }
func (w *captureWriter) Size() int                        { return w.Body.Len() }
func (w *captureWriter) Written() bool                    { return true }
func (w *captureWriter) WriteHeaderNow()                  {}
func (w *captureWriter) Flush()                           {}
func (w *captureWriter) Pusher() http.Pusher              { return nil }
func (w *captureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack not supported")
}
func (w *captureWriter) CloseNotify() <-chan bool { return make(chan bool, 1) }

// emitSSETo writes one event the same way helper.ClaudeData does: the event
// line and the data block are separate writes.
func emitSSETo(w gin.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

type sseEvent struct {
	Event string
	Data  string
}

// parseSSE splits a captured body back into events.
func parseSSE(body string) []sseEvent {
	var events []sseEvent
	var cur sseEvent
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			cur.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			cur.Data += strings.TrimPrefix(line, "data:")
		case line == "":
			if cur.Event != "" || cur.Data != "" {
				events = append(events, cur)
			}
			cur = sseEvent{}
		}
	}
	return events
}

func dataField(t *testing.T, data, field string) interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := common.Unmarshal([]byte(data), &m); err != nil {
		t.Fatalf("bad event data %q: %v", data, err)
	}
	return m[field]
}

// ---------- interceptor golden cases ----------

func newTestInterceptor(virtual map[string]bool, offset int, first bool) (*StreamInterceptor, *captureWriter) {
	w := newCaptureWriter()
	lw := &lockedWriter{w: w}
	return newStreamInterceptor(lw, virtual, offset, first), w
}

func TestInterceptorTextOnlyFirstIteration(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{"web_search": true}, 0, true)
	w.Header().Set("Content-Type", "text/event-stream")

	emitSSETo(i, "message_start", `{"type":"message_start","message":{"id":"msg_1","model":"glm-4.6","usage":{"input_tokens":10,"output_tokens":1}}}`)
	emitSSETo(i, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	emitSSETo(i, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}`)
	emitSSETo(i, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	emitSSETo(i, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
	emitSSETo(i, "message_stop", `{"type":"message_stop"}`)

	res := &i.result
	if res.Fatal || res.StreamError {
		t.Fatalf("unexpected failure flags: %+v", res)
	}
	if res.BlockCount != 1 {
		t.Errorf("BlockCount = %d, want 1", res.BlockCount)
	}
	if len(res.TextBlocks) != 1 || res.TextBlocks[0].Text != "你好" {
		t.Errorf("TextBlocks = %+v", res.TextBlocks)
	}
	if res.StopReason != "end_turn" || res.MessageID != "msg_1" || res.Model != "glm-4.6" {
		t.Errorf("result meta = %+v", res)
	}
	if res.Usage.InputTokens != 10 || res.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v", res.Usage)
	}

	events := parseSSE(w.Body.String())
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Event)
	}
	// message_start forwarded (zeroed usage), block forwarded, framing swallowed.
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("forwarded events = %v, want %v", kinds, want)
	}
	if !strings.Contains(events[0].Data, `"input_tokens":0`) {
		t.Errorf("first message_start not zeroed: %s", events[0].Data)
	}
}

func TestInterceptorVirtualToolCapture(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{"web_search": true}, 2, false) // iteration >= 2
	w.Header().Set("Content-Type", "text/event-stream")

	emitSSETo(i, "message_start", `{"type":"message_start","message":{"id":"msg_2","usage":{"input_tokens":99}}}`)
	emitSSETo(i, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"srvtoolu_1","name":"web_search","input":{}}}`)
	emitSSETo(i, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"que"}}`)
	emitSSETo(i, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ry\":\"AAPL\"}"}}`)
	emitSSETo(i, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	emitSSETo(i, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`)
	emitSSETo(i, "message_stop", `{"type":"message_stop"}`)

	res := &i.result
	if len(res.VirtualCalls) != 1 {
		t.Fatalf("VirtualCalls = %d, want 1", len(res.VirtualCalls))
	}
	call := res.VirtualCalls[0]
	if call.ID != "srvtoolu_1" || call.Name != "web_search" {
		t.Errorf("call = %+v", call)
	}
	if call.Input["query"] != "AAPL" {
		t.Errorf("accumulated input = %v, want query=AAPL", call.Input)
	}
	if res.BlockCount != 0 {
		t.Errorf("suppressed block must not count, got %d", res.BlockCount)
	}
	if res.HasClientToolUse {
		t.Error("virtual call must not set HasClientToolUse")
	}

	if out := w.Body.String(); out != "" {
		t.Errorf("non-first iteration with only a virtual call must forward nothing, got:\n%s", out)
	}
}

func TestInterceptorMalformedInputJsonFallsBackToRaw(t *testing.T) {
	i, _ := newTestInterceptor(map[string]bool{"web_search": true}, 0, true)
	i.Header().Set("Content-Type", "text/event-stream")

	emitSSETo(i, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"srvtoolu_2","name":"web_search","input":{}}}`)
	emitSSETo(i, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"not json"}}`)
	emitSSETo(i, "content_block_stop", `{"type":"content_block_stop","index":0}`)

	res := &i.result
	if len(res.VirtualCalls) != 1 {
		t.Fatalf("VirtualCalls = %d, want 1", len(res.VirtualCalls))
	}
	if res.VirtualCalls[0].RawInput != "not json" || res.VirtualCalls[0].Input != nil {
		t.Errorf("malformed json must fall back to RawInput: %+v", res.VirtualCalls[0])
	}
}

func TestInterceptorThinkingMixed(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{"web_search": true}, 0, true)
	w.Header().Set("Content-Type", "text/event-stream")

	emitSSETo(i, "message_start", `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1}}}`)
	emitSSETo(i, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
	emitSSETo(i, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"让我想想"}}`)
	emitSSETo(i, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	emitSSETo(i, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
	emitSSETo(i, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"答案"}}`)
	emitSSETo(i, "content_block_stop", `{"type":"content_block_stop","index":1}`)

	res := &i.result
	if res.BlockCount != 2 {
		t.Errorf("BlockCount = %d, want 2", res.BlockCount)
	}
	// thinking text must NOT be captured as assistant text for the next body
	if len(res.TextBlocks) != 1 || res.TextBlocks[0].Text != "答案" {
		t.Errorf("TextBlocks = %+v", res.TextBlocks)
	}
	if !strings.Contains(w.Body.String(), "thinking_delta") {
		t.Error("thinking events must be forwarded unchanged")
	}
}

func TestInterceptorClientToolUse(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{"web_search": true}, 0, true)
	w.Header().Set("Content-Type", "text/event-stream")

	emitSSETo(i, "message_start", `{"type":"message_start","message":{"id":"m"}}`)
	emitSSETo(i, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_9","name":"get_weather","input":{}}}`)
	emitSSETo(i, "content_block_stop", `{"type":"content_block_stop","index":0}`)

	res := &i.result
	if !res.HasClientToolUse {
		t.Error("client tool_use must set HasClientToolUse")
	}
	if res.BlockCount != 1 {
		t.Errorf("BlockCount = %d, want 1", res.BlockCount)
	}
	if !strings.Contains(w.Body.String(), "get_weather") {
		t.Error("client tool_use must be forwarded")
	}
}

func TestInterceptorIndexOffset(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{}, 5, false)
	w.Header().Set("Content-Type", "text/event-stream")

	emitSSETo(i, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
	emitSSETo(i, "content_block_stop", `{"type":"content_block_stop","index":0}`)

	events := parseSSE(w.Body.String())
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if got := dataField(t, events[0].Data, "index"); got != float64(5) {
		t.Errorf("offset index = %v, want 5", got)
	}
}

func TestInterceptorStreamError(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{}, 0, true)
	w.Header().Set("Content-Type", "text/event-stream")

	emitSSETo(i, "message_start", `{"type":"message_start","message":{}}`)
	emitSSETo(i, "error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)

	res := &i.result
	if !res.StreamError || !res.StreamStarted {
		t.Errorf("flags = %+v", res)
	}
	if !strings.Contains(w.Body.String(), "overloaded_error") {
		t.Error("error event must be forwarded verbatim")
	}
}

func TestInterceptorNonJSONDataInStream(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{}, 0, true)
	w.Header().Set("Content-Type", "text/event-stream")

	// relay wrote a JSON error body (no event framing, no trailing newline)
	fmt.Fprint(i, `{"error":{"message":"insufficient quota"}}`)
	i.finish()

	res := &i.result
	if !res.StreamError {
		t.Error("non-SSE JSON in stream must set StreamError")
	}
	out := w.Body.String()
	if !strings.Contains(out, "event: error") || !strings.Contains(out, "insufficient quota") {
		t.Errorf("expected translated SSE error event, got:\n%s", out)
	}
}

func TestInterceptorPassthroughMode(t *testing.T) {
	i, w := newTestInterceptor(map[string]bool{}, 0, true)
	w.Header().Set("Content-Type", "application/json") // non-SSE upstream response

	body := `{"error":{"type":"authentication_error","message":"invalid api key"}}`
	n, err := i.Write([]byte(body))
	if err != nil || n != len(body) {
		t.Fatalf("write failed: %v", err)
	}

	if !i.result.Fatal {
		t.Error("non-SSE response must set Fatal")
	}
	if got := w.Body.String(); got != body {
		t.Errorf("passthrough body = %q", got)
	}
}

// ---------- loop integration with a stub relay ----------

const testTool = "test_echo_tool"

type fakeExecutor struct {
	calls []string
}

func (f *fakeExecutor) Name() string { return testTool }
func (f *fakeExecutor) Execute(sess *Session, call *CapturedCall) *ExecResult {
	query, _ := call.Param("query")
	f.calls = append(f.calls, query)
	return &ExecResult{
		Block: []interface{}{
			map[string]interface{}{"type": "test_result", "query": query, "answer": "42"},
		},
		ModelText:   "ECHO:" + query + "=42",
		StatusShort: "ok",
	}
}

// stubRelay plays a scripted SSE sequence. Each script step receives the gin
// context (writing goes to the loop's interceptor) and the body the loop fed
// to this iteration, so later steps can assert on re-injected history.
type stubRelay struct {
	t      *testing.T
	script []func(c *gin.Context, body map[string]interface{})
	step   int
	bodies []map[string]interface{}
}

func (s *stubRelay) invoke(c *gin.Context) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		s.t.Fatalf("relay: no body storage: %v", err)
	}
	data, err := storage.Bytes()
	if err != nil {
		s.t.Fatalf("relay: body read: %v", err)
	}
	var body map[string]interface{}
	if err := common.Unmarshal(data, &body); err != nil {
		s.t.Fatalf("relay: bad body: %v", err)
	}
	s.bodies = append(s.bodies, body)

	if s.step >= len(s.script) {
		s.t.Fatalf("relay invoked more times than scripted (step %d)", s.step)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	fn := s.script[s.step]
	s.step++
	fn(c, body)
}

func newLoopContext(t *testing.T, body map[string]interface{}) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	data, err := common.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	storage, err := common.CreateBodyStorage(data)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	c.Set(common.KeyBodyStorage, storage)
	c.Request = httptest.NewRequest(http.MethodPost, "/excel/v1/messages", nil)
	c.Set("id", 7)
	return c, rec
}

func baseLoopBody() map[string]interface{} {
	return map[string]interface{}{
		"model":      "glm-4.6",
		"max_tokens": 100,
		"stream":     true,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "今天黄金多少钱"},
		},
	}
}

func testPlan() *LoopPlan {
	return &LoopPlan{
		Active:        true,
		VirtualNames:  map[string]bool{testTool: true},
		MaxIterations: 3,
		PingInterval:  50 * time.Millisecond,
	}
}

func TestLoopSingleVirtualCall(t *testing.T) {
	excel_tools.ResetForTest()
	defer excel_tools.ResetForTest()

	fake := &fakeExecutor{}
	RegisterExecutor(fake)
	defer delete(executors, testTool)

	relay := &stubRelay{t: t}
	relay.script = []func(c *gin.Context, body map[string]interface{}){
		// iteration 1: text + virtual tool call
		func(c *gin.Context, body map[string]interface{}) {
			w := c.Writer
			emitSSETo(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1}}}`)
			emitSSETo(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
			emitSSETo(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"我去查一下"}}`)
			emitSSETo(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
			emitSSETo(w, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"srvtoolu_1","name":"test_echo_tool","input":{}}}`)
			emitSSETo(w, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"金价\"}"}}`)
			emitSSETo(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
			emitSSETo(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`)
			emitSSETo(w, "message_stop", `{"type":"message_stop"}`)
		},
		// iteration 2: must contain re-injected tool_use + tool_result history
		func(c *gin.Context, body map[string]interface{}) {
			msgs, _ := body["messages"].([]interface{})
			if len(msgs) != 3 {
				t.Errorf("iteration-2 body should have 3 messages, got %d", len(msgs))
			}
			assistant, _ := msgs[1].(map[string]interface{})
			user, _ := msgs[2].(map[string]interface{})
			aJSON, _ := common.Marshal(assistant)
			uJSON, _ := common.Marshal(user)
			if !strings.Contains(string(aJSON), "test_echo_tool") {
				t.Errorf("iteration-2 body missing assistant tool_use: %s", aJSON)
			}
			if !strings.Contains(string(aJSON), "我去查一下") {
				t.Errorf("iteration-2 body missing assistant text: %s", aJSON)
			}
			if !strings.Contains(string(uJSON), "ECHO:金价=42") {
				t.Errorf("iteration-2 body missing tool_result text: %s", uJSON)
			}

			w := c.Writer
			emitSSETo(w, "message_start", `{"type":"message_start","message":{"id":"m2","usage":{"input_tokens":20,"output_tokens":1}}}`)
			emitSSETo(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
			emitSSETo(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"今天是42元"}}`)
			emitSSETo(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
			emitSSETo(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`)
			emitSSETo(w, "message_stop", `{"type":"message_stop"}`)
		},
	}

	c, rec := newLoopContext(t, baseLoopBody())
	RunToolLoop(c, testPlan(), relay.invoke)

	if len(fake.calls) != 1 || fake.calls[0] != "金价" {
		t.Errorf("executor calls = %v", fake.calls)
	}

	out := rec.Body.String()
	if n := strings.Count(out, "event: message_start"); n != 1 {
		t.Errorf("client must see exactly one message_start, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, "event: message_stop"); n != 1 {
		t.Errorf("client must see exactly one message_stop, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, `"server_tool_use"`) || !strings.Contains(out, "test_echo_tool") {
		t.Errorf("client must see the synthesized server_tool_use block:\n%s", out)
	}
	if !strings.Contains(out, `"answer":"42"`) || !strings.Contains(out, `"金价"`) {
		t.Errorf("client must see the structured result block content:\n%s", out)
	}
	// aggregated usage in the final message_delta: 10+20 input, 5+7 output
	if !strings.Contains(out, `"input_tokens":30`) || !strings.Contains(out, `"output_tokens":12`) {
		t.Errorf("final message_delta must carry aggregated usage:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Errorf("final stop reason missing:\n%s", out)
	}
	// iteration-2 blocks must be re-indexed after iteration-1's text block (0)
	// + 2 synthesized blocks (1,2) => iteration-2 text lands on index 3
	if !strings.Contains(out, `"index":3`) {
		t.Errorf("iteration-2 text block should carry index 3:\n%s", out)
	}
}

func TestLoopStubbornReachesRejection(t *testing.T) {
	excel_tools.ResetForTest()
	defer excel_tools.ResetForTest()

	fake := &fakeExecutor{}
	RegisterExecutor(fake)
	defer delete(executors, testTool)

	n := 0
	relay := &stubRelay{t: t}
	relay.script = []func(c *gin.Context, body map[string]interface{}){
		func(c *gin.Context, body map[string]interface{}) {
			w := c.Writer
			id := fmt.Sprintf("srvtoolu_%d", n)
			n++
			emitSSETo(w, "message_start", `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1}}}`)
			emitSSETo(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"%s","name":"test_echo_tool","input":{}}}`, id))
			emitSSETo(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"q\"}"}}`)
			emitSSETo(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
			emitSSETo(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`)
			emitSSETo(w, "message_stop", `{"type":"message_stop"}`)
		},
		func(c *gin.Context, body map[string]interface{}) {},
		func(c *gin.Context, body map[string]interface{}) {},
	}
	relay.script[1] = relay.script[0]
	relay.script[2] = relay.script[0]

	c, rec := newLoopContext(t, baseLoopBody())
	RunToolLoop(c, testPlan(), relay.invoke)

	// executor ran once (iteration 1); iterations 2/3 are budget rejections
	if len(fake.calls) != 1 {
		t.Errorf("executor calls = %d, want 1", len(fake.calls))
	}
	out := rec.Body.String()
	if n := strings.Count(out, "额度已用完"); n != 2 {
		t.Errorf("expected 2 visible rejection blocks (iter 2+3), got %d:\n%s", n, out)
	}
	if !strings.Contains(out, `"stop_reason":"end_turn"`) {
		t.Errorf("final iteration must force end_turn:\n%s", out)
	}
	if strings.Count(out, "event: message_start") != 1 || strings.Count(out, "event: message_stop") != 1 {
		t.Errorf("client framing corrupted:\n%s", out)
	}
}

func TestLoopClientToolTerminates(t *testing.T) {
	excel_tools.ResetForTest()
	defer excel_tools.ResetForTest()

	fake := &fakeExecutor{}
	RegisterExecutor(fake)
	defer delete(executors, testTool)

	relay := &stubRelay{t: t}
	relay.script = []func(c *gin.Context, body map[string]interface{}){
		func(c *gin.Context, body map[string]interface{}) {
			w := c.Writer
			emitSSETo(w, "message_start", `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":3}}}`)
			// client tool + virtual tool in the same iteration
			emitSSETo(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_c","name":"client_tool","input":{}}}`)
			emitSSETo(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
			emitSSETo(w, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"srvtoolu_1","name":"test_echo_tool","input":{}}}`)
			emitSSETo(w, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"x\"}"}}`)
			emitSSETo(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
			emitSSETo(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`)
			emitSSETo(w, "message_stop", `{"type":"message_stop"}`)
		},
	}

	c, rec := newLoopContext(t, baseLoopBody())
	RunToolLoop(c, testPlan(), relay.invoke)

	if len(relay.bodies) != 1 {
		t.Errorf("relay invoked %d times, want 1 (client tool ends the turn)", len(relay.bodies))
	}
	if len(fake.calls) != 1 {
		t.Errorf("virtual call should still execute once, got %d", len(fake.calls))
	}
	out := rec.Body.String()
	if !strings.Contains(out, "client_tool") {
		t.Errorf("client tool block missing:\n%s", out)
	}
	if !strings.Contains(out, `"answer":"42"`) {
		t.Errorf("virtual result block missing:\n%s", out)
	}
	if !strings.Contains(out, `"stop_reason":"tool_use"`) {
		t.Errorf("client stop reason must be preserved:\n%s", out)
	}
	if strings.Count(out, "event: message_start") != 1 || strings.Count(out, "event: message_stop") != 1 {
		t.Errorf("client framing corrupted:\n%s", out)
	}
}

func TestLoopRelayFatalPassthrough(t *testing.T) {
	excel_tools.ResetForTest()
	defer excel_tools.ResetForTest()

	c, rec := newLoopContext(t, baseLoopBody())
	RunToolLoop(c, testPlan(), func(cc *gin.Context) {
		cc.Writer.Header().Set("Content-Type", "application/json")
		_, _ = cc.Writer.Write([]byte(`{"error":{"type":"authentication_error","message":"bad key"}}`))
	})

	out := rec.Body.String()
	if !strings.Contains(out, "authentication_error") {
		t.Errorf("fatal relay error must pass through untouched:\n%s", out)
	}
	if strings.Contains(out, "event: message_stop") {
		t.Errorf("no synthesized closing frames after a fatal error:\n%s", out)
	}
}
