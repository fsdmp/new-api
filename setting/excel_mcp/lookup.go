package excel_mcp

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Lookup helpers over the configured server list. The parsed list is cheap
// (a handful of entries) so no extra cache layer beyond the config manager's
// own raw-string equality check is needed.

// ListEnabled returns the enabled servers in configured order.
func ListEnabled() []MCPServer {
	var servers []MCPServer
	raw := defaultSettings.Servers
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	if err := common.Unmarshal([]byte(raw), &servers); err != nil {
		common.SysError("failed to parse excel_mcp.servers: " + err.Error())
		return nil
	}
	result := make([]MCPServer, 0, len(servers))
	for _, s := range servers {
		if s.Enabled && ValidServerId(s.Id) {
			result = append(result, s)
		}
	}
	return result
}

// FindServer resolves one server by id among the enabled entries.
func FindServer(id string) *MCPServer {
	servers := ListEnabled()
	for i := range servers {
		if servers[i].Id == id {
			return &servers[i]
		}
	}
	return nil
}

// EffectiveTimeout clamps the per-call proxy timeout.
func (s *MCPServer) EffectiveTimeout() int {
	if s.TimeoutSec <= 0 {
		return 300
	}
	if s.TimeoutSec > 600 {
		return 600
	}
	return s.TimeoutSec
}

// IsBlocked reports whether a tool is org-blocked via tool_policies.
func (s *MCPServer) IsBlocked(tool string) bool {
	return s.ToolPolicies[tool] == "blocked"
}

// SanitizeName mirrors the add-in's i0(): lowercase, non-alnum runs -> "_",
// trimmed leading/trailing underscores (mcp__<sanitized>__<tool> prefix).
func SanitizeName(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
		} else {
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
