package middleware

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_tools"
)

func applyMode(t *testing.T, mode string, searchJSON string) {
	t.Helper()
	excel_tools.ApplyForTest(excel_tools.ToolsSettings{
		Mode:   mode,
		Search: searchJSON,
		Fetch:  `{"timeout_sec":15,"max_body_bytes":5242880,"daily_quota":100}`,
		Sandbox: `{"enabled":false,"nsjail_path":"","rootfs_path":"","timeout_sec":30,"max_concurrent":4,"daily_quota":50}`,
		Loop:   `{"max_iterations":5,"ping_interval_sec":2}`,
	})
	t.Cleanup(excel_tools.ResetForTest)
}

const searchWithKey = `{"provider":"zhipu","api_keys":{"zhipu":"test-key"},"max_results":5,"daily_quota":50}`

func virtualToolsRequest() map[string]interface{} {
	return map[string]interface{}{
		"model": "glm-4.6",
		"tools": []interface{}{
			// virtual server tool, vendor versioned type (real vendor shapes:
			// web_search_20250305 / code_execution_20250825 / web_fetch_20250910)
			map[string]interface{}{
				"type":     "web_search_20250305",
				"name":     "web_search",
				"max_uses": 3,
			},
			// virtual server tool without key backing
			map[string]interface{}{
				"type": "code_execution_20250825",
				"name": "code_execution",
			},
			// web_fetch carries max_content_tokens (vendor declaration)
			map[string]interface{}{
				"type":               "web_fetch_20250910",
				"name":               "web_fetch",
				"max_content_tokens": 100000,
			},
			// unknown server tool (vendor upgrade sentinel)
			map[string]interface{}{
				"type": "memory_20260101",
				"name": "memory",
			},
			// regular client-executed tool
			map[string]interface{}{
				"name":        "get_weather",
				"description": "local weather",
				"input_schema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
				},
			},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user", "content": "你好",
			},
		},
	}
}

func TestSanitizeToolsFullModeInjectsSchema(t *testing.T) {
	applyMode(t, excel_tools.ModeFull, searchWithKey)

	cleaned, info := sanitizeExcelBody(virtualToolsRequest())
	if info == nil {
		t.Fatal("VirtualToolInfo expected when virtual tools present")
	}
	// web_search injected (search key configured) + web_fetch (no deps)
	if len(info.Names) != 2 || info.Names[0] != excel_tools.ToolWebSearch || info.Names[1] != excel_tools.ToolWebFetch {
		t.Errorf("Names = %v, want [web_search web_fetch]", info.Names)
	}
	if info.MaxUsages[excel_tools.ToolWebSearch] != 3 {
		t.Errorf("max_uses not captured: %v", info.MaxUsages)
	}
	// vendor web_fetch declaration field flows through to the session
	if info.MaxContentTokens[excel_tools.ToolWebFetch] != 100000 {
		t.Errorf("max_content_tokens not captured: %v", info.MaxContentTokens)
	}
	// code_execution stripped (sandbox unavailable: enabled=false)
	if len(info.Stripped) != 1 || info.Stripped[0] != excel_tools.ToolCodeExecution {
		t.Errorf("Stripped = %v, want [code_execution]", info.Stripped)
	}

	tools, _ := cleaned["tools"].([]interface{})
	var names []string
	for _, tool := range tools {
		tm, _ := tool.(map[string]interface{})
		names = append(names, tm["name"].(string))
		if tm["name"] == "web_search" {
			if tm["description"] == nil || tm["input_schema"] == nil {
				t.Errorf("injected web_search lacks description/schema: %v", tm)
			}
			if _, hasType := tm["type"]; hasType {
				t.Errorf("injected tool must not carry server type: %v", tm)
			}
		}
	}
	want := "web_search web_fetch get_weather"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("tools = %q, want %q", got, want)
	}
}

func TestSanitizeToolsStripModeRemovesVirtual(t *testing.T) {
	applyMode(t, excel_tools.ModeStrip, "")

	body := virtualToolsRequest()
	cleaned, info := sanitizeExcelBody(body)
	injectExcelChineseLanguage(cleaned)
	injectExcelToolsNotes(cleaned, info)

	if info == nil {
		t.Fatal("VirtualToolInfo expected")
	}
	if len(info.Names) != 0 {
		t.Errorf("strip mode must inject nothing, got %v", info.Names)
	}
	tools, _ := cleaned["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("strip mode must keep only the regular tool, got %d: %v", len(tools), tools)
	}
	if tools[0].(map[string]interface{})["name"] != "get_weather" {
		t.Errorf("regular tool lost: %v", tools[0])
	}

	// degradation note injected into the system prompt
	sys, _ := cleaned["system"].(string)
	if !strings.Contains(sys, "excel-tools-note") || !strings.Contains(sys, "没有联网搜索") {
		t.Errorf("strip-mode degradation note missing:\n%s", sys)
	}
}

func TestSanitizeToolsOffModeLegacyPassthrough(t *testing.T) {
	applyMode(t, excel_tools.ModeOff, "")

	cleaned, info := sanitizeExcelBody(virtualToolsRequest())
	if info != nil && len(info.Names) != 0 {
		t.Errorf("off mode must not inject tools, got %+v", info)
	}
	tools, _ := cleaned["tools"].([]interface{})
	if len(tools) != 5 {
		t.Fatalf("off mode keeps all 5 tools with legacy empty schemas, got %d: %v", len(tools), tools)
	}
	var names []string
	for _, tool := range tools {
		tm, _ := tool.(map[string]interface{})
		names = append(names, tm["name"].(string))
		schema, hasSchema := tm["input_schema"].(map[string]interface{})
		if !hasSchema || len(schema) != 2 { // legacy empty schema shape
			t.Errorf("off mode must emit empty schema for %v", tm)
		}
	}
	if strings.Join(names, ",") != "web_search,code_execution,web_fetch,memory,get_weather" {
		t.Errorf("off mode tool names = %v", names)
	}
}

func TestHistoryServerToolBlocksConverted(t *testing.T) {
	applyMode(t, excel_tools.ModeStrip, "") // any mode != off converts

	body := map[string]interface{}{
		"model": "glm-4.6",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "查一下金价"},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type": "server_tool_use", "id": "srvtoolu_1",
						"name": "web_search", "input": map[string]interface{}{"query": "金价"},
					},
				},
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "web_search_tool_result", "tool_use_id": "srvtoolu_1",
						"content": []interface{}{
							map[string]interface{}{"type": "web_search_result", "title": "金价新闻", "url": "https://example.com"},
						},
					},
				},
			},
		},
	}
	cleaned, _ := sanitizeExcelBody(body)
	msgs, _ := cleaned["messages"].([]interface{})
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	assistant := msgs[1].(map[string]interface{})
	ablock := assistant["content"].([]interface{})[0].(map[string]interface{})
	if ablock["type"] != "tool_use" || ablock["id"] != "srvtoolu_1" || ablock["name"] != "web_search" {
		t.Errorf("server_tool_use conversion wrong: %v", ablock)
	}

	user := msgs[2].(map[string]interface{})
	ublock := user["content"].([]interface{})[0].(map[string]interface{})
	if ublock["type"] != "tool_result" || ublock["tool_use_id"] != "srvtoolu_1" {
		t.Errorf("tool_result conversion wrong: %v", ublock)
	}
	content, _ := ublock["content"].(string)
	if !strings.Contains(content, "金价新闻") {
		t.Errorf("tool_result content must be JSON-stringified, got %q", content)
	}
	// the stringified content must round-trip as valid JSON
	var parsed interface{}
	if err := common.Unmarshal([]byte(content), &parsed); err != nil {
		t.Errorf("stringified content not valid JSON: %v", err)
	}
}

func TestHistoryServerToolBlocksDroppedInOffMode(t *testing.T) {
	applyMode(t, excel_tools.ModeOff, "")

	body := map[string]interface{}{
		"model": "glm-4.6",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "查一下金价"},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "server_tool_use", "id": "srvtoolu_1", "name": "web_search", "input": map[string]interface{}{}},
					map[string]interface{}{"type": "text", "text": "好的"},
				},
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "web_search_tool_result", "tool_use_id": "srvtoolu_1", "content": "x"},
				},
			},
		},
	}
	cleaned, _ := sanitizeExcelBody(body)
	msgs, _ := cleaned["messages"].([]interface{})
	if len(msgs) != 2 {
		t.Fatalf("off mode must drop server-tool history blocks, got %d messages", len(msgs))
	}
	// assistant message keeps only the text block
	ablocks := msgs[1].(map[string]interface{})["content"].([]interface{})
	if len(ablocks) != 1 || ablocks[0].(map[string]interface{})["type"] != "text" {
		t.Errorf("assistant blocks = %v", ablocks)
	}
}

func TestTruncateForLog(t *testing.T) {
	excel_tools.ApplyForTest(excel_tools.ToolsSettings{Mode: excel_tools.ModeStrip, LogBodyMaxBytes: 16})
	t.Cleanup(excel_tools.ResetForTest)

	if got := truncateForLog([]byte("short")); got != "short" {
		t.Errorf("short body altered: %q", got)
	}
	got := truncateForLog([]byte("0123456789abcdefghij"))
	if !strings.Contains(got, "truncated 16/20 bytes") {
		t.Errorf("truncation marker missing: %q", got)
	}
	if len(got) > 16+64 {
		t.Errorf("truncated log too long: %d", len(got))
	}

	excel_tools.ApplyForTest(excel_tools.ToolsSettings{Mode: excel_tools.ModeStrip, LogBodyMaxBytes: -1})
	if got := truncateForLog([]byte("anything")); got != "<disabled>" {
		t.Errorf("negative limit must disable, got %q", got)
	}
}
