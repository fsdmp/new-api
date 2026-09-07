package exceltools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/excel_mcp"
)

func TestMCPAllowedMethod(t *testing.T) {
	for _, m := range []string{"initialize", "ping", "tools/list", "tools/call", "notifications/initialized"} {
		if !MCPAllowedMethod(m) {
			t.Errorf("MCPAllowedMethod(%q) = false", m)
		}
	}
	for _, m := range []string{"resources/read", "prompts/get", "completion/complete", "shutdown", ""} {
		if MCPAllowedMethod(m) {
			t.Errorf("MCPAllowedMethod(%q) = true, want false", m)
		}
	}
}

func TestMCPExtractMethod(t *testing.T) {
	if got := MCPExtractMethod([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)); got != "tools/list" {
		t.Errorf("MCPExtractMethod = %q", got)
	}
	if got := MCPExtractMethod([]byte(`not json`)); got != "" {
		t.Errorf("invalid body must yield empty method, got %q", got)
	}
}

func TestMCPRegistryResponseDisabledAndEmpty(t *testing.T) {
	excel_mcp.ApplyForTest(excel_mcp.Settings{Enabled: false, Servers: `[]`, ToolsCacheSec: 60, MaxConcurrent: 4})
	t.Cleanup(excel_mcp.ResetForTest)
	resp := MCPRegistryResponse()
	if data, ok := resp["data"].([]mcpServerEntry); !ok || len(data) != 0 {
		t.Errorf("disabled registry must return empty data, got %v", resp)
	}
}

func TestMCPRegistryProbesUpstream(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("mcp-session-id"))
		if r.Header.Get("mcp-session-id") == "" {
			// initialize — hand out a session id like a real streamable server
			w.Header().Set("mcp-session-id", "sess-123")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[
			{"name":"search","description":"Search code","inputSchema":{"type":"object","properties":{}}}
		]}}`))
	}))
	defer srv.Close()

	mcpCache = &mcpRegistryCache{entries: map[string]mcpCacheEntry{}} // isolate cache
	excel_mcp.ApplyForTest(excel_mcp.Settings{
		Enabled: true,
		Servers: `[{"id":"test","display_name":"Test","url":"` + srv.URL + `","enabled":true,"tool_policies":{"search":"blocked"}}]`,
	})
	t.Cleanup(excel_mcp.ResetForTest)

	resp := MCPRegistryResponse()
	entries, ok := resp["data"].([]mcpServerEntry)
	if !ok || len(entries) != 1 {
		t.Fatalf("registry data = %v", resp)
	}
	e := entries[0]
	if len(e.Tools) != 1 || e.Tools[0].Name != "search" {
		t.Fatalf("tools = %+v, want one 'search'", e.Tools)
	}
	if e.Tools[0].EffectiveMaxPermission != "blocked" {
		t.Errorf("policy not surfaced: %+v", e.Tools[0])
	}
	if len(seen) != 2 || seen[0] != "" || seen[1] != "sess-123" {
		t.Errorf("handshake session header flow broken: %v", seen)
	}

	// second call must be served from cache (no new upstream requests)
	n := len(seen)
	_ = MCPRegistryResponse()
	if len(seen) != n {
		t.Errorf("cache miss on second registry read: %d -> %d requests", n, len(seen))
	}
}

func TestMCPProbeTimeoutDoesNotBlockRegistry(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer dead.Close()

	mcpCache = &mcpRegistryCache{entries: map[string]mcpCacheEntry{}}
	excel_mcp.ApplyForTest(excel_mcp.Settings{
		Enabled: true,
		Servers: `[{"id":"dead","display_name":"Dead","url":"` + dead.URL + `","enabled":true}]`,
	})
	t.Cleanup(excel_mcp.ResetForTest)

	start := time.Now()
	resp := MCPRegistryResponse()
	if time.Since(start) > 8*time.Second {
		t.Fatalf("registry blocked %v on dead upstream", time.Since(start))
	}
	entries := resp["data"].([]mcpServerEntry)
	if len(entries) != 1 || len(entries[0].Tools) != 0 {
		t.Errorf("dead upstream must yield entry with zero tools, got %+v", entries)
	}
}

func TestMCPAcquireRespectsContext(t *testing.T) {
	mcpSemOnce = sync.Once{}
	mcpSem = nil
	excel_mcp.ApplyForTest(excel_mcp.Settings{MaxConcurrent: 2})
	t.Cleanup(excel_mcp.ResetForTest)

	ctx := context.Background()
	r1, ok := mcpAcquire(ctx)
	if !ok {
		t.Fatal("first acquire failed")
	}
	r2, ok := mcpAcquire(ctx)
	if !ok {
		t.Fatal("second acquire failed")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := mcpAcquire(cancelled); ok {
		t.Error("mcpAcquire must honor cancelled context when full")
	}
	r1()
	r2()
}
