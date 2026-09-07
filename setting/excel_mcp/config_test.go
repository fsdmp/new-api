package excel_mcp

import (
	"strings"
	"testing"
)

func TestValidServerId(t *testing.T) {
	for _, ok := range []string{"a", "github", "deep-wiki", "srv_01", strings.Repeat("x", 64)} {
		if !ValidServerId(ok) {
			t.Errorf("ValidServerId(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "UPPER", "sp ace", "sl/ash", "do.t", "uni码", strings.Repeat("x", 65)} {
		if ValidServerId(bad) {
			t.Errorf("ValidServerId(%q) = true, want false", bad)
		}
	}
}

func TestListEnabledAndFind(t *testing.T) {
	ApplyForTest(Settings{
		Enabled: true,
		Servers: `[
			{"id":"github","display_name":"GitHub","url":"https://mcp.example.com/gh","enabled":true},
			{"id":"disabled-one","display_name":"Off","url":"https://mcp.example.com/off","enabled":false},
			{"id":"Bad Id","display_name":"Bad","url":"https://mcp.example.com/bad","enabled":true}
		]`,
		ToolsCacheSec: 60,
		MaxConcurrent: 4,
	})
	t.Cleanup(ResetForTest)

	servers := ListEnabled()
	if len(servers) != 1 || servers[0].Id != "github" {
		t.Fatalf("ListEnabled() = %+v, want only github (invalid id + disabled filtered)", servers)
	}
	if FindServer("github") == nil {
		t.Error("FindServer(github) = nil")
	}
	if FindServer("disabled-one") != nil || FindServer("Bad Id") != nil {
		t.Error("FindServer must only resolve enabled + valid-id servers")
	}
}

func TestServerTimeoutAndPolicies(t *testing.T) {
	s := &MCPServer{Id: "x"}
	if s.EffectiveTimeout() != 300 {
		t.Errorf("default timeout = %d, want 300", s.EffectiveTimeout())
	}
	s.TimeoutSec = 99999
	if s.EffectiveTimeout() != 600 {
		t.Errorf("clamped timeout = %d, want 600", s.EffectiveTimeout())
	}
	if s.IsBlocked("tool") {
		t.Error("nil policies must not block")
	}
	s.ToolPolicies = map[string]string{"danger": "blocked"}
	if !s.IsBlocked("danger") || s.IsBlocked("other") {
		t.Error("IsBlocked policy mismatch")
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"GitHub":        "github",
		"Deep Wiki":     "deep_wiki",
		"  --odd--  ":   "odd",
		"v2.0 Beta":     "v2_0_beta",
		"多个字":           "", // non-ascii collapses to underscores, then trimmed empty
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("a", 100)
	if got := SanitizeName(long); len(got) != 64 {
		t.Errorf("SanitizeName long = %d chars, want capped 64", len(got))
	}
}
