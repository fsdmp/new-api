package middleware

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/excel_setting"
	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// ---- document block conversion + image passthrough (M2) ----

func documentRequest() map[string]interface{} {
	return map[string]interface{}{
		"model": "glm-4.6",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "document",
						"source": map[string]interface{}{
							"type":    "file",
							"file_id": "file-0123456789abcdef01234567",
						},
					},
					map[string]interface{}{"type": "text", "text": "帮我读一下这个文件"},
				},
			},
		},
	}
}

func TestDocumentBlockConvertedToHint(t *testing.T) {
	applyMode(t, excel_tools.ModeFull, searchWithKey)
	applyFilesForTest(t, true)

	cleaned, _ := sanitizeExcelBody(documentRequest(), 7)
	blocks := cleaned["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	first := blocks[0].(map[string]interface{})
	if first["type"] != "text" {
		t.Fatalf("document must become a text block, got %v", first)
	}
	// no DB row for this id/user -> not-found hint (never a passthrough)
	if !strings.Contains(first["text"].(string), "文件不存在或已过期") {
		t.Errorf("expected not-found hint, got %q", first["text"])
	}
}

func TestDocumentBlockDisabledHintWhenServiceOff(t *testing.T) {
	applyMode(t, excel_tools.ModeFull, searchWithKey)
	applyFilesForTest(t, false)

	cleaned, _ := sanitizeExcelBody(documentRequest(), 7)
	blocks := cleaned["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	if got := blocks[0].(map[string]interface{})["text"].(string); !strings.Contains(got, "未启用文件服务") {
		t.Errorf("expected disabled hint, got %q", got)
	}
}

func TestInlineBase64DocumentGetsInlineHint(t *testing.T) {
	applyMode(t, excel_tools.ModeFull, searchWithKey)
	applyFilesForTest(t, true)

	body := documentRequest()
	body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["source"] = map[string]interface{}{
		"type":       "base64",
		"media_type": "application/pdf",
		"data":       "JVBERi0=",
	}
	cleaned, _ := sanitizeExcelBody(body, 7)
	blocks := cleaned["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	if got := blocks[0].(map[string]interface{})["text"].(string); !strings.Contains(got, "内嵌文档") {
		t.Errorf("expected inline hint, got %q", got)
	}
}

func TestImageBlockPassesThrough(t *testing.T) {
	applyMode(t, excel_tools.ModeFull, searchWithKey)
	applyFilesForTest(t, true)

	body := map[string]interface{}{
		"model": "glm-4.6",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type": "base64", "media_type": "image/png", "data": "aGk=",
						},
					},
				},
			},
		},
	}
	cleaned, _ := sanitizeExcelBody(body, 7)
	blocks := cleaned["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	if len(blocks) != 1 || blocks[0].(map[string]interface{})["type"] != "image" {
		t.Fatalf("image block must survive sanitization, got %v", blocks)
	}
	if !excelBodyContainsImage(cleaned) {
		t.Error("excelBodyContainsImage must detect the image block")
	}
	if excelBodyContainsImage(documentRequest()) {
		t.Error("document-only request must not count as image")
	}
}

func TestVisionRoutingReroutesTextOnlyModel(t *testing.T) {
	applyMode(t, excel_tools.ModeFull, searchWithKey)
	applyFilesForTest(t, true)
	excel_setting.ApplyModelListForTest(`[
		{"id":"glm-text","display_name":"Text","target_model":"glm-4.6-text","enabled":true,"vision":false},
		{"id":"glm-vision","display_name":"Vision","target_model":"glm-4.6v","enabled":true}
	]`)
	t.Cleanup(func() { excel_setting.ApplyModelListForTest("[]") })

	body := map[string]interface{}{
		"model": "glm-text",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "image", "source": map[string]interface{}{
						"type": "base64", "media_type": "image/png", "data": "aGk=",
					}},
				},
			},
		},
	}
	cleaned, _ := sanitizeExcelBody(body, 7)
	applyVisionRouting(cleaned)
	if cleaned["model"] != "glm-vision" {
		t.Errorf("image to text-only model must reroute to vision alias, got %v", cleaned["model"])
	}
}

func TestVisionRoutingKeepsTextRequestOnTextModel(t *testing.T) {
	applyMode(t, excel_tools.ModeFull, searchWithKey)
	applyFilesForTest(t, true)
	excel_setting.ApplyModelListForTest(`[
		{"id":"glm-text","display_name":"Text","target_model":"glm-4.6-text","enabled":true,"vision":false},
		{"id":"glm-vision","display_name":"Vision","target_model":"glm-4.6v","enabled":true}
	]`)
	t.Cleanup(func() { excel_setting.ApplyModelListForTest("[]") })

	body := map[string]interface{}{
		"model": "glm-text",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "纯文本问题"},
		},
	}
	cleaned, _ := sanitizeExcelBody(body, 7)
	applyVisionRouting(cleaned)
	if cleaned["model"] != "glm-text" {
		t.Errorf("text request must stay on text model, got %v", cleaned["model"])
	}
}

func TestVisionRoutingNoVisionAliasLeavesRequest(t *testing.T) {
	applyFilesForTest(t, true)
	excel_setting.ApplyModelListForTest(`[
		{"id":"m1","display_name":"M1","target_model":"","enabled":true,"vision":false},
		{"id":"m2","display_name":"M2","target_model":"","enabled":true,"vision":false}
	]`)
	t.Cleanup(func() { excel_setting.ApplyModelListForTest("[]") })

	body := map[string]interface{}{
		"model": "m1",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "image", "source": map[string]interface{}{
						"type": "base64", "media_type": "image/png", "data": "aGk=",
					}},
				},
			},
		},
	}
	applyVisionRouting(body)
	if body["model"] != "m1" {
		t.Errorf("no vision alias configured must leave model untouched, got %v", body["model"])
	}
}

func applyFilesForTest(t *testing.T, enabled bool) {
	t.Helper()
	excel_tools.ApplyForTest(excel_tools.ToolsSettings{
		Mode:   excel_tools.ModeFull,
		Search: searchWithKey,
		Files:  `{"enabled":` + boolJSON(enabled) + `,"max_size_mb":20,"max_files_per_user":100,"max_mb_per_user":500,"ttl_days":30,"extensions":["pdf","csv","xlsx"],"max_file_ids_per_run":5}`,
	})
	t.Cleanup(excel_tools.ResetForTest)
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
