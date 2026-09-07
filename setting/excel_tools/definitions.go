package excel_tools

import (
	"strings"
)

// GinKeyVirtualTools is the gin context key under which the excel adapter
// stashes VirtualToolInfo for the tool-loop controller.
const GinKeyVirtualTools = "excel_virtual_tools"

// VirtualToolInfo summarizes how the virtual server tools in one request were
// handled by the sanitizer. Written by middleware, read by the loop controller.
type VirtualToolInfo struct {
	// Names are the virtual tools actually injected as callable functions (full mode).
	Names []string
	// MaxUsages maps tool name -> per-request max_uses from the client's tool
	// declaration (0 = unset / unlimited by request).
	MaxUsages map[string]int
	// MaxContentTokens maps tool name -> per-request max_content_tokens from
	// the client's declaration (web_fetch uses it today; 0 = unset).
	MaxContentTokens map[string]int
	// Stripped are the virtual tools removed from the request (strip mode or
	// executor unavailable).
	Stripped []string
}

// Virtual tool names (the `name` field the client declares / the model calls).
const (
	ToolWebSearch     = "web_search"
	ToolWebFetch      = "web_fetch"
	ToolCodeExecution = "code_execution"
)

// ToolDefinition is the single source of truth for a virtual server tool:
// the type prefix used to recognize it in client requests, and the
// name/description/input_schema injected in full mode.
type ToolDefinition struct {
	Name        string
	TypePrefix  string // e.g. "web_search_" (matches web_search_20250305 and future versions)
	Description string
	InputSchema map[string]interface{}
}

// VirtualToolPrefixes lists the recognized server-tool type prefixes.
var VirtualToolPrefixes = []string{"web_search_", "web_fetch_", "code_execution_"}

var toolDefinitions = []ToolDefinition{
	{
		Name:       ToolWebSearch,
		TypePrefix: "web_search_",
		Description: "Search the web for current information (prices, news, recent events). " +
			"Returns a list of results with title, url and snippet. " +
			"Use for facts you are unsure about or that may be outdated. Use at most once per turn unless truly necessary. " +
			"When the question is about \"today\" or \"latest\" and the current date is stated in the context, " +
			"include that date in the query so stale pages do not outrank fresh ones; " +
			"never invent a date — if unsure, search without one. " +
			"If a query returns no results, retry once with shorter, more common wording.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query, in the same language as the expected results",
				},
			},
			"required": []string{"query"},
		},
	},
	{
		Name:       ToolWebFetch,
		TypePrefix: "web_fetch_",
		Description: "Fetch a web page by absolute http(s) URL and return its main content as markdown. " +
			"Use to read pages found via web_search results.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "Absolute http(s) URL to fetch",
				},
			},
			"required": []string{"url"},
		},
	},
	{
		Name:       ToolCodeExecution,
		TypePrefix: "code_execution_",
		Description: "Run a Python script and return stdout/stderr. " +
			"pandas, numpy, openpyxl, pdfplumber, python-docx and lxml are preinstalled. " +
			"Use for precise computation and statistics instead of mental math. " +
			"Write a complete script, print results, and do not read external network resources.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code": map[string]interface{}{
					"type":        "string",
					"description": "Complete Python script; print the results you need",
				},
				"file_ids": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional: file ids of files uploaded in this conversation (from /excel/v1/files). After passing them here, each file is readable read-only at /files/input/<file_id>/<filename> inside the sandbox",
				},
			},
			"required": []string{"code"},
		},
	},
}

// ToolDefinitions exposes the full definition list (tests iterate it to
// assert schema shape).
func ToolDefinitions() []ToolDefinition { return toolDefinitions }

// MatchVirtualTool resolves a tool `type` string (e.g. "web_search_20250305")
// to its definition by prefix, surviving vendor version-suffix drift.
func MatchVirtualTool(typeStr string) *ToolDefinition {
	for i := range toolDefinitions {
		if strings.HasPrefix(typeStr, toolDefinitions[i].TypePrefix) {
			return &toolDefinitions[i]
		}
	}
	return nil
}

// MatchToolByName resolves a tool by its call name (e.g. "web_search").
func MatchToolByName(name string) *ToolDefinition {
	for i := range toolDefinitions {
		if toolDefinitions[i].Name == name {
			return &toolDefinitions[i]
		}
	}
	return nil
}

// IsVirtualToolName reports whether name is one of the three virtual tools.
func IsVirtualToolName(name string) bool {
	return MatchToolByName(name) != nil
}

// ResultBlockType returns the *_tool_result content block type for a tool name.
func ResultBlockType(name string) string {
	switch name {
	case ToolWebSearch:
		return "web_search_tool_result"
	case ToolWebFetch:
		return "web_fetch_tool_result"
	case ToolCodeExecution:
		return "code_execution_tool_result"
	}
	return ""
}
