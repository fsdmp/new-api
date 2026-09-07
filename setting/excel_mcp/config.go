// Package excel_mcp holds configuration for the M4 MCP connector registry and
// proxy (GET /excel/v1/mcp_servers, POST /excel/v1/mcp/{server_id}), following
// the excel_tools registration pattern: flat scalars top-level, the server
// list stored as a JSON string so the generic config manager round-trips it.
//
// The add-in discovers servers here and then JSON-RPCs them THROUGH the
// gateway proxy, so browser CORS never sees a third-party origin (design doc
// docs/excel-mcp-skills-m4m5-design.md §2.1).
package excel_mcp

import (
	"github.com/QuantumNous/new-api/setting/config"
)

// MCPServer is one upstream connector entry (stored inside the servers JSON).
type MCPServer struct {
	// Id is the URL segment the add-in proxies through (/excel/v1/mcp/<id>).
	// Restrict to ^[a-z0-9_-]{1,64}$.
	Id string `json:"id"`
	// DisplayName is what the UI shows; the add-in also sanitizes it into the
	// mcp__<sanitized>__<tool> tool-name prefix.
	DisplayName string `json:"display_name"`
	// Url is the upstream streamable-HTTP MCP endpoint (https enforced in
	// production). Admin-configured = trusted; never user input.
	URL string `json:"url"`
	// Headers are static auth headers sent to the upstream on every proxy
	// request (e.g. {"Authorization":"Bearer ..."}). Never logged.
	Headers map[string]string `json:"headers,omitempty"`
	Enabled bool              `json:"enabled"`
	// TimeoutSec caps tools/call proxy requests; others use 30s. Default 300.
	TimeoutSec int `json:"timeout_sec"`
	// ToolPolicies optionally maps tool name -> "blocked" (rendered as
	// effective_max_permission in the registry response so the add-in hides /
	// refuses the tool).
	ToolPolicies map[string]string `json:"tool_policies,omitempty"`
}

// Settings is registered under config key "excel_mcp".
type Settings struct {
	Enabled bool `json:"enabled"`
	// Servers is the JSON string of []MCPServer (flat-scalar round-trip rule).
	Servers string `json:"servers"`
	// ToolsCacheSec is how long the registry caches upstream tools/list
	// metadata. Default 300.
	ToolsCacheSec int `json:"tools_cache_sec"`
	// MaxConcurrent caps in-flight proxy requests. Default 16.
	MaxConcurrent int `json:"max_concurrent"`
}

const defaultServersJSON = `[]`

var defaultSettings = Settings{
	Enabled:       false,
	Servers:       defaultServersJSON,
	ToolsCacheSec: 300,
	MaxConcurrent: 16,
}

func init() {
	config.GlobalConfig.Register("excel_mcp", &defaultSettings)
}

// Config returns the live settings view.
func Config() Settings { return defaultSettings }

// ValidServerId reports whether id is safe as a URL path segment.
func ValidServerId(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// ApplyForTest overwrites the live settings in place. Unit tests only — the
// config manager keeps writing the same pointer, so field assignment keeps
// both views consistent.
func ApplyForTest(s Settings) {
	defaultSettings.Enabled = s.Enabled
	defaultSettings.Servers = s.Servers
	defaultSettings.ToolsCacheSec = s.ToolsCacheSec
	defaultSettings.MaxConcurrent = s.MaxConcurrent
}

// ResetForTest restores factory defaults (pair with ApplyForTest).
func ResetForTest() {
	ApplyForTest(Settings{
		Enabled:       false,
		Servers:       defaultServersJSON,
		ToolsCacheSec: 300,
		MaxConcurrent: 16,
	})
}
