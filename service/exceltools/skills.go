package exceltools

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// M5 skill cloud sync (docs/excel-mcp-skills-m4m5-design.md §2.3-2.4). Skills
// are admin- or user-uploaded zips containing a SKILL.md; the add-in lists,
// downloads and reads them via /api/oauth/organizations/<org>/skills/*.

const (
	skillsRoot       = "skills"
	maxSkillZipBytes = 10 << 20 // upload cap: 10MB
	maxSkillExtract  = 30 << 20 // total decompressed cap (zip-bomb guard)
	maxSkillFiles    = 500      // entries per zip
	maxSkillInstrBts = 2 << 20  // SKILL.md text kept in DB
	// MaxSkillsPerUserQuota caps a user's own uploads.
	MaxSkillsPerUserQuota = 50
)

// skillIDRe / skillNameRe guard the two user-controlled identifiers.
var (
	skillIDRe   = regexp.MustCompile(`^skill_[0-9a-f]{24}$`)
	skillNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
)

// NewExcelSkillID generates a gateway-side id ("skill_" + 24 hex) — the
// add-in only client-downloads skills whose id carries this prefix.
func NewExcelSkillID() (string, error) {
	buf := make([]byte, 12) // 12 bytes -> 24 hex
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "skill_" + hex.EncodeToString(buf), nil
}

// ValidExcelSkillID reports whether id matches the generated shape.
func ValidExcelSkillID(id string) bool { return skillIDRe.MatchString(id) }

// ValidSkillName reports whether a frontmatter name is acceptable.
func ValidSkillName(name string) bool { return skillNameRe.MatchString(name) }

// ExcelSkillBodyPath returns the zip storage path (relative to workdir, same
// convention as the M2 file bodies).
func ExcelSkillBodyPath(skillID string) string {
	return filepath.Join(skillsRoot, skillID, "skill.zip")
}

// ParsedSkill is the outcome of parsing one uploaded zip.
type ParsedSkill struct {
	Name         string
	Description  string
	Instructions string
	ZipBytes     []byte
}

// ParseSkillZip validates and extracts the SKILL.md from an uploaded archive.
// Hardened against Zip Slip (entry paths must stay inside the target) and
// zip bombs (per-file + total decompressed caps).
func ParseSkillZip(raw []byte, fallbackName string) (*ParsedSkill, error) {
	if len(raw) > maxSkillZipBytes {
		return nil, fmt.Errorf("zip 超过 %dMB 上限", maxSkillZipBytes>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("不是有效的 zip 文件")
	}
	if len(zr.File) > maxSkillFiles {
		return nil, fmt.Errorf("zip 内文件数超过 %d", maxSkillFiles)
	}

	skillMD := ""
	var total int64
	for _, f := range zr.File {
		// Zip Slip: clean each entry and require it to stay relative.
		cleaned := filepath.Clean(f.Name)
		if strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "..") ||
			strings.Contains(cleaned, "../") || (len(cleaned) >= 2 && cleaned[1] == ':') {
			return nil, fmt.Errorf("zip 内含非法路径 %q", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("zip 条目 %q 无法读取", f.Name)
		}
		// cap per-file read at maxSkillExtract to bound any single entry
		data, err := io.ReadAll(io.LimitReader(rc, maxSkillExtract))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("zip 条目 %q 读取失败", f.Name)
		}
		total += int64(len(data))
		if total > maxSkillExtract {
			return nil, fmt.Errorf("zip 解压总量超过 %dMB", maxSkillExtract>>20)
		}
		base := strings.ToLower(filepath.Base(cleaned))
		if base == "skill.md" && skillMD == "" {
			if len(data) > maxSkillInstrBts {
				return nil, fmt.Errorf("SKILL.md 超过 %dMB", maxSkillInstrBts>>20)
			}
			skillMD = string(data)
		}
	}
	if skillMD == "" {
		return nil, fmt.Errorf("zip 中未找到 SKILL.md")
	}

	name, description := parseSkillFrontmatter(skillMD)
	if name == "" {
		name = fallbackName
	}
	if !ValidSkillName(name) {
		return nil, fmt.Errorf("技能名 %q 无效（仅允许字母数字-_，≤64 字符）", name)
	}
	return &ParsedSkill{Name: name, Description: description, Instructions: skillMD, ZipBytes: raw}, nil
}

// parseSkillFrontmatter extracts name/description from a leading YAML block:
//   ---
//   name: my-skill
//   description: what it does
//   ---
func parseSkillFrontmatter(md string) (name, description string) {
	if !strings.HasPrefix(md, "---") {
		return "", ""
	}
	end := strings.Index(md[3:], "\n---")
	if end < 0 {
		return "", ""
	}
	block := md[3 : end+3]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	// strip surrounding quotes if present
	strip := func(s string) string {
		if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
			return s[1 : len(s)-1]
		}
		return s
	}
	return strip(name), strip(description)
}

// SaveExcelSkill persists a parsed skill: zip to disk + metadata row. When
// overwrite is set the existing org+name row is replaced atomically enough for
// our scale (update-in-place, zip path rewritten).
func SaveExcelSkill(orgId string, userId int, parsed *ParsedSkill, partitionBy string, overwrite bool) (*model.ExcelSkill, error) {
	if partitionBy != "organization" {
		partitionBy = "account"
	}
	existing, err := model.ExcelSkillNameExists(orgId, parsed.Name)
	if err != nil {
		return nil, err
	}
	if existing && !overwrite {
		return nil, ErrSkillNameConflict
	}

	id, err := NewExcelSkillID()
	if err != nil {
		return nil, err
	}
	bodyPath := ExcelSkillBodyPath(id)
	if err := os.MkdirAll(filepath.Dir(bodyPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(bodyPath, parsed.ZipBytes, 0o644); err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	row := &model.ExcelSkill{
		Id:           id,
		OrgId:        orgId,
		Name:         parsed.Name,
		Description:  parsed.Description,
		Instructions: parsed.Instructions,
		Enabled:      true,
		CreatorType:  "user",
		IsShared:     false,
		PartitionBy:  partitionBy,
		OwnerUserId:  userId,
		ZipPath:      bodyPath,
		SizeBytes:    int64(len(parsed.ZipBytes)),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := model.CreateExcelSkill(row); err != nil {
		os.Remove(bodyPath)
		return nil, err
	}
	return row, nil
}

// DeleteExcelSkillBody removes the stored zip (best-effort — row delete is
// the source of truth).
func DeleteExcelSkillBody(skillID string) {
	_ = os.Remove(ExcelSkillBodyPath(skillID))
	_ = os.Remove(filepath.Dir(ExcelSkillBodyPath(skillID))) // empty dir cleanup
}

// OpenExcelSkillZip opens the stored zip for download.
func OpenExcelSkillZip(s *model.ExcelSkill) (*os.File, error) {
	return os.Open(s.ZipPath)
}

// ErrSkillNameConflict marks the upload conflict case (HTTP 409).
var ErrSkillNameConflict = fmt.Errorf("同名技能已存在")

// MaxSkillZipBytes exposes the upload cap to the controller layer.
func MaxSkillZipBytes() int { return maxSkillZipBytes }

// MaxSkillsPerUser exposes the per-user upload quota to the controller layer.
func MaxSkillsPerUser() int { return MaxSkillsPerUserQuota }
