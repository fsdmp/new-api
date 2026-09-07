package exceltools

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestSkillIDAndNameShapes(t *testing.T) {
	id, err := NewExcelSkillID()
	if err != nil {
		t.Fatalf("NewExcelSkillID: %v", err)
	}
	if !ValidExcelSkillID(id) {
		t.Errorf("generated id %q fails own validation", id)
	}
	if ValidExcelSkillID("skill_zzz") || ValidExcelSkillID("abc") {
		t.Error("IDs without skill_+24hex must be rejected")
	}
	if !ValidSkillName("My-Skill_2") || ValidSkillName("-lead") || ValidSkillName("空格 x") {
		t.Error("ValidSkillName mismatch")
	}
}

func TestParseSkillFrontmatter(t *testing.T) {
	const md = "---\nname: \"quarterly-report\"\ndescription: builds Q reports\nother: x\n---\n# body\n"
	name, desc := parseSkillFrontmatter(md)
	if name != "quarterly-report" || desc != "builds Q reports" {
		t.Errorf("frontmatter = %q/%q", name, desc)
	}
	if n, d := parseSkillFrontmatter("no frontmatter here"); n != "" || d != "" {
		t.Errorf("plain text should yield empty, got %q/%q", n, d)
	}
}

// buildTestZip is a tiny in-memory zip builder for ParseSkillZip cases.
func buildTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestParseSkillZipHappyPath(t *testing.T) {
	raw := buildTestZip(t, map[string]string{
		"my-skill/SKILL.md":     "---\nname: my-skill\ndescription: demo\n---\nbody",
		"my-skill/scripts/x.sh": "echo hi",
	})
	p, err := ParseSkillZip(raw, "fallback")
	if err != nil {
		t.Fatalf("ParseSkillZip: %v", err)
	}
	if p.Name != "my-skill" || p.Description != "demo" {
		t.Errorf("parsed = %q/%q", p.Name, p.Description)
	}
	if len(p.ZipBytes) != len(raw) {
		t.Error("ZipBytes must carry the original archive verbatim")
	}
}

func TestParseSkillZipRejects(t *testing.T) {
	cases := map[string][]byte{
		"not a zip":         []byte("garbage-not-a-zip"),
		"missing SKILL.md":  buildTestZip(t, map[string]string{"a/readme.txt": "hi"}),
		"zip slip":          buildTestZip(t, map[string]string{"../evil.md": "x"}),
		"abs path":          buildTestZip(t, map[string]string{"/abs.md": "x"}),
	}
	for name, raw := range cases {
		if _, err := ParseSkillZip(raw, "fb"); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
	if _, err := ParseSkillZip(buildTestZip(t, map[string]string{"BAD NAME.md": "x"}), "fb"); err == nil {
		t.Error("invalid fallback name must be rejected")
	}
}

func TestParseSkillZipFallbackName(t *testing.T) {
	raw := buildTestZip(t, map[string]string{"SKILL.md": "no frontmatter body"})
	p, err := ParseSkillZip(raw, "fallback-name")
	if err != nil {
		t.Fatalf("ParseSkillZip: %v", err)
	}
	if p.Name != "fallback-name" {
		t.Errorf("fallback name = %q", p.Name)
	}
}

func TestSkillBodyPathAndMaxes(t *testing.T) {
	if !strings.HasPrefix(ExcelSkillBodyPath("skill_x"), skillsRoot+"/") {
		t.Errorf("body path = %q", ExcelSkillBodyPath("skill_x"))
	}
	if MaxSkillZipBytes() != maxSkillZipBytes || MaxSkillsPerUser() != MaxSkillsPerUserQuota {
		t.Error("accessor mismatch")
	}
}
