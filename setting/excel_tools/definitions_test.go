package excel_tools

import (
	"strings"
	"testing"
)

func TestMatchVirtualToolPrefix(t *testing.T) {
	cases := []struct {
		typeStr string
		want    string // expected tool name, "" for no match
	}{
		{"web_search_", ToolWebSearch},          // bare prefix (defensive)
		{"web_search_20250305", ToolWebSearch},   // vendor bundle 54022
		{"web_search_20991231", ToolWebSearch},   // future version drift
		{"web_fetch_20250910", ToolWebFetch},     // vendor bundle 54023
		{"code_execution_20250825", ToolCodeExecution}, // vendor bundle 258377
		{"web_search20250305", ""},   // missing separator — NOT a prefix match
		{"web_searcher", ""},         // different tool
		{"computer_20250224", ""},    // unknown server capability
		{"bash_20250124", ""},
		{"", ""},
		{"custom", ""},
	}
	for _, tc := range cases {
		def := MatchVirtualTool(tc.typeStr)
		got := ""
		if def != nil {
			got = def.Name
		}
		if got != tc.want {
			t.Errorf("MatchVirtualTool(%q) = %q, want %q", tc.typeStr, got, tc.want)
		}
	}
}

func TestMatchToolByName(t *testing.T) {
	for _, name := range []string{ToolWebSearch, ToolWebFetch, ToolCodeExecution} {
		if !IsVirtualToolName(name) {
			t.Errorf("IsVirtualToolName(%q) = false, want true", name)
		}
		def := MatchToolByName(name)
		if def == nil || def.Name != name {
			t.Errorf("MatchToolByName(%q) mismatch", name)
		}
		if def.InputSchema == nil || def.Description == "" {
			t.Errorf("tool %q lacks schema or description", name)
		}
	}
	for _, name := range []string{"get_weather", "", "web_search_v2"} {
		if IsVirtualToolName(name) {
			t.Errorf("IsVirtualToolName(%q) = true, want false", name)
		}
	}
}

func TestResultBlockType(t *testing.T) {
	cases := map[string]string{
		ToolWebSearch:     "web_search_tool_result",
		ToolWebFetch:      "web_fetch_tool_result",
		ToolCodeExecution: "code_execution_tool_result",
		"other":           "",
	}
	for in, want := range cases {
		if got := ResultBlockType(in); got != want {
			t.Errorf("ResultBlockType(%q) = %q, want %q", in, got, want)
		}
	}
}

// The code_execution description promises specific preinstalled packages —
// it must stay in sync with deploy/excel-sandbox/Dockerfile.
func TestCodeExecutionDescriptionMentionsPreinstalledLibs(t *testing.T) {
	def := MatchToolByName(ToolCodeExecution)
	for _, lib := range []string{"pandas", "numpy", "openpyxl", "pdfplumber", "python-docx", "lxml"} {
		if !strings.Contains(def.Description, lib) {
			t.Errorf("code_execution description missing preinstalled lib %q (must match deploy/excel-sandbox/Dockerfile)", lib)
		}
	}
}
