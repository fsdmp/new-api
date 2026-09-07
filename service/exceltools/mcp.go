package exceltools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_mcp"
)

// M4 MCP connector support (docs/excel-mcp-skills-m4m5-design.md §2.2):
// the add-in fetches the registry (GET /excel/v1/mcp_servers) and then
// JSON-RPCs each server THROUGH our proxy (POST /excel/v1/mcp/<id>) so the
// browser never talks to a third-party origin. Tool execution itself happens
// client-side in the add-in — the /messages relay pipeline is untouched.

const (
	// mcpBodyBudget caps one proxied response body.
	mcpBodyBudget = 32 << 20
	// mcpProbeTimeout bounds the registry's upstream tools/list probe.
	mcpProbeTimeout = 3 * time.Second
	// mcpDefaultTimeout bounds proxy calls whose method could not be sniffed.
	mcpDefaultTimeout = 30 * time.Second
)

// mcpToolMeta is the registry-visible shape of one upstream tool.
type mcpToolMeta struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
	// EffectiveMaxPermission carries the admin's tool_policies decision
	// ("blocked") — the add-in turns this into per-tool refusal.
	EffectiveMaxPermission string `json:"effective_max_permission,omitempty"`
}

// mcpServerEntry is one registry response item.
type mcpServerEntry struct {
	Id          string        `json:"id"`
	DisplayName string        `json:"display_name"`
	URL         string        `json:"url"`
	IconURL     string        `json:"icon_url,omitempty"`
	Tools       []mcpToolMeta `json:"tools"`
}

// mcpRegistryCache holds upstream tools/list metadata keyed by a hash of
// (url + headers) so config edits invalidate only via TTL.
type mcpRegistryCache struct {
	mu      sync.Mutex
	entries map[string]mcpCacheEntry
}

type mcpCacheEntry struct {
	tools     []mcpToolMeta
	fetchedAt time.Time
}

var mcpCache = &mcpRegistryCache{entries: map[string]mcpCacheEntry{}}

// mcpSem caps in-flight proxy/probe requests (buffered channel; size fixed at
// first use, 64 hard ceiling so a config typo can't overload the gateway).
var (
	mcpSemOnce sync.Once
	mcpSem     chan struct{}
)

// mcpAcquire reserves a slot, honoring ctx cancellation. The returned release
// func must be called exactly once.
func mcpAcquire(ctx context.Context) (func(), bool) {
	mcpSemOnce.Do(func() {
		n := excel_mcp.Config().MaxConcurrent
		if n <= 0 {
			n = 16
		}
		if n > 64 {
			n = 64
		}
		mcpSem = make(chan struct{}, n)
	})
	select {
	case mcpSem <- struct{}{}:
		return func() { <-mcpSem }, true
	case <-ctx.Done():
		return nil, false
	}
}

func mcpCacheKey(url string, headers map[string]string) string {
	h := sha256.New()
	h.Write([]byte(url))
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte{0})
		h.Write([]byte(k))
		h.Write([]byte(headers[k]))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// mcpClient is the shared HTTP client for upstream MCP traffic.
var mcpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	},
}

// MCPRegistryResponse builds the /excel/v1/mcp_servers payload: every enabled
// server with its (cached, best-effort) tool metadata. A server whose upstream
// probe fails still appears — with zero tools — so the UI can show it and
// retry; availability errors must not blank the connector list.
func MCPRegistryResponse() map[string]interface{} {
	servers := excel_mcp.ListEnabled()
	entries := make([]mcpServerEntry, 0, len(servers))
	ttl := time.Duration(excel_mcp.Config().ToolsCacheSec) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	for _, s := range servers {
		entry := mcpServerEntry{
			Id:          s.Id,
			DisplayName: s.DisplayName,
			URL:         s.URL,
			Tools:       []mcpToolMeta{},
		}
		tools, ok := mcpCache.get(s.URL, s.Headers, ttl)
		if !ok {
			var err error
			tools, err = mcpProbeTools(s.URL, s.Headers)
			if err != nil {
				common.SysLog("[excel-mcp] tools/list probe failed for " + s.Id + ": " + err.Error())
			} else {
				mcpCache.put(s.URL, s.Headers, tools)
			}
		}
		for _, t := range tools {
			if s.IsBlocked(t.Name) {
				t.EffectiveMaxPermission = "blocked"
			}
			entry.Tools = append(entry.Tools, t)
		}
		entries = append(entries, entry)
	}
	return map[string]interface{}{
		"data":      entries,
		"next_page": nil,
	}
}

func (c *mcpRegistryCache) get(url string, headers map[string]string, ttl time.Duration) ([]mcpToolMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[mcpCacheKey(url, headers)]
	if !ok || time.Since(e.fetchedAt) > ttl {
		return nil, false
	}
	return e.tools, true
}

func (c *mcpRegistryCache) put(url string, headers map[string]string, tools []mcpToolMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[mcpCacheKey(url, headers)] = mcpCacheEntry{tools: tools, fetchedAt: time.Now()}
}

// mcpProbeTools performs a minimal initialize + tools/list handshake against
// the upstream to fill the registry metadata (the add-in repeats the same
// handshake through the proxy on connect).
func mcpProbeTools(url string, headers map[string]string) ([]mcpToolMeta, error) {
	release, ok := mcpAcquire(context.Background())
	if !ok {
		return nil, fmt.Errorf("registry probe concurrency limit")
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), mcpProbeTimeout*2)
	defer cancel()

	doRPC := func(body string, session string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		mcpApplyHeaders(req, headers)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("mcp-session-id", session)
		}
		return mcpClient.Do(req)
	}

	resp, err := doRPC(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"new-api-registry","version":"1.0.0"}}}`, "")
	if err != nil {
		return nil, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)) // drain; session header is what matters
	resp.Body.Close()
	session := resp.Header.Get("mcp-session-id")

	resp2, err := doRPC(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, session)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tools/list status %d", resp2.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp2.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Result struct {
			Tools []struct {
				Name        string                 `json:"name"`
				Description string                 `json:"description"`
				InputSchema map[string]interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := mcpParseRPCResponse(raw, &parsed); err != nil {
		return nil, err
	}
	tools := make([]mcpToolMeta, 0, len(parsed.Result.Tools))
	for _, t := range parsed.Result.Tools {
		tools = append(tools, mcpToolMeta{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return tools, nil
}

// mcpParseRPCResponse decodes either a bare JSON-RPC body or the first result
// event of an SSE stream (data: {...} lines) — mirrors the add-in's parser.
func mcpParseRPCResponse(raw []byte, v interface{}) error {
	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if strings.HasPrefix(trimmed, "{") {
		return common.Unmarshal(raw, v)
	}
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var probe map[string]interface{}
		if err := common.Unmarshal([]byte(payload), &probe); err != nil {
			continue
		}
		if _, has := probe["result"]; has {
			return common.Unmarshal([]byte(payload), v)
		}
	}
	return fmt.Errorf("no JSON-RPC result found in response")
}

// MCPProxyTimeout picks the proxy timeout for one JSON-RPC method: tools/call
// gets the server's configured (long) budget, everything else 30s.
func MCPProxyTimeout(server *excel_mcp.MCPServer, method string) time.Duration {
	if method == "tools/call" {
		return time.Duration(server.EffectiveTimeout()) * time.Second
	}
	return mcpDefaultTimeout
}

// MCPProxyRequest forwards one JSON-RPC call to the upstream server
// (stateless: the add-in echoes back the mcp-session-id we handed it, so the
// client session maps 1:1 onto the upstream session without gateway state).
func MCPProxyRequest(ctx context.Context, server *excel_mcp.MCPServer, body io.Reader, clientSession, contentType, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, body)
	if err != nil {
		return nil, err
	}
	mcpApplyHeaders(req, server.Headers)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if clientSession != "" {
		req.Header.Set("mcp-session-id", clientSession)
	}
	return mcpClient.Do(req)
}

// mcpApplyHeaders sets the admin-configured upstream headers (never logged).
func mcpApplyHeaders(req *http.Request, headers map[string]string) {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// MCPBodyBudget caps the proxied response body size.
func MCPBodyBudget() int64 { return mcpBodyBudget }

// MCPAcquire reserves a proxy slot (exported for the controller).
func MCPAcquire(ctx context.Context) (func(), bool) { return mcpAcquire(ctx) }

// MCPAllowedMethod reports whether a JSON-RPC method may be proxied.
func MCPAllowedMethod(method string) bool {
	switch {
	case method == "initialize", method == "ping":
		return true
	case strings.HasPrefix(method, "tools/"):
		return true
	case strings.HasPrefix(method, "notifications/"):
		return true
	}
	return false
}

// MCPExtractMethod best-effort sniffs the JSON-RPC method from the request
// body (timeout tiering); a parse failure is not fatal for the proxy.
func MCPExtractMethod(body []byte) string {
	var probe struct {
		Method string `json:"method"`
	}
	if err := common.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.Method
}
