package exceltools

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/excel_tools"
)

func applyFilesConfig(t *testing.T, cfg string) {
	t.Helper()
	excel_tools.ApplyForTest(excel_tools.ToolsSettings{Mode: excel_tools.ModeFull, Files: cfg})
	t.Cleanup(excel_tools.ResetForTest)
}

const testFilesJSON = `{"enabled":true,"max_size_mb":20,"max_files_per_user":100,"max_mb_per_user":500,"ttl_days":30,"extensions":["pdf","csv","xlsx","png"],"max_file_ids_per_run":5}`

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"report.pdf":            "report.pdf",
		"/etc/passwd":           "passwd",
		"..\\..\\evil.csv":      "evil.csv",
		"../../evil.csv":        "evil.csv",
		"a\x00b\x1fc.xlsx":      "abc.xlsx",
		"   ":                   "upload.bin",
		".":                     "upload.bin",
		"..":                    "upload.bin",
		"":                      "upload.bin",
		"with space v2 (1).pdf": "with space v2 (1).pdf",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("长", 200) // 600 bytes
	if got := SanitizeFilename(long); len(got) > maxFilenameBytes {
		t.Errorf("long name not capped: %d bytes", len(got))
	}
}

func TestValidExcelFileID(t *testing.T) {
	if !ValidExcelFileID("file-0123456789abcdef01234567") {
		t.Error("well-formed id rejected")
	}
	for _, bad := range []string{
		"", "file-", "file-0123456789abcdef012345678", "file-0123456789ABCDEF01234567",
		"file-../../etc/passwd", "../files/1/file-x", "file-0123456789abcdef0123456\x00",
	} {
		if ValidExcelFileID(bad) {
			t.Errorf("malformed id accepted: %q", bad)
		}
	}
	id, err := NewExcelFileID()
	if err != nil || !ValidExcelFileID(id) {
		t.Errorf("NewExcelFileID produced %q (err=%v)", id, err)
	}
}

func TestExtensionWhitelist(t *testing.T) {
	applyFilesConfig(t, testFilesJSON)
	for _, ok := range []string{"a.pdf", "b.XLSX", "c.csv", "d.png"} {
		if !extensionAllowed(ok) {
			t.Errorf("%q should be allowed", ok)
		}
	}
	for _, bad := range []string{"a.exe", "b.sh", "noext", "c.php.pdf.exe", "d.html"} {
		if extensionAllowed(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestDocumentHintsCoverStates(t *testing.T) {
	applyFilesConfig(t, testFilesJSON)
	block := map[string]interface{}{
		"type":   "document",
		"source": map[string]interface{}{"type": "file", "file_id": "file-0123456789abcdef01234567"},
	}
	// no DB row (model layer unavailable in unit tests) -> not-found hint
	if got := DocumentHintForBlock(block, 1); !strings.Contains(got, "文件不存在或已过期") {
		t.Errorf("want not-found hint, got %q", got)
	}
	// service disabled
	applyFilesConfig(t, `{"enabled":false,"max_size_mb":20,"max_files_per_user":100,"max_mb_per_user":500,"ttl_days":30,"extensions":["pdf"],"max_file_ids_per_run":5}`)
	if got := DocumentHintForBlock(block, 1); !strings.Contains(got, "未启用文件服务") {
		t.Errorf("want disabled hint, got %q", got)
	}
}

func TestCodeExecutionSchemaHasFileIds(t *testing.T) {
	applyFilesConfig(t, testFilesJSON)
	for _, def := range excel_tools.ToolDefinitions() {
		if def.Name != excel_tools.ToolCodeExecution {
			continue
		}
		props, ok := def.InputSchema["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("schema properties missing: %v", def.InputSchema)
		}
		if _, ok := props["file_ids"]; !ok {
			t.Error("code_execution schema lacks file_ids property")
		}
		if _, ok := props["code"]; !ok {
			t.Error("code_execution schema lacks code property")
		}
	}
}

func TestSanitizePurpose(t *testing.T) {
	if got := SanitizePurpose(""); got != "default" {
		t.Errorf("empty purpose = %q", got)
	}
	if got := SanitizePurpose(strings.Repeat("x", 100)); len(got) != 64 {
		t.Errorf("purpose not capped: %d", len(got))
	}
}
