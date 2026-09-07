package model

import (
	"errors"

	"gorm.io/gorm"
)

// ExcelSkill is one provisioned skill (M5, docs/excel-mcp-skills-m4m5-design.md
// §2.3). The add-in lists skills per organization from
// /api/oauth/organizations/<org>/skills/list-skills; only ids with the
// "skill_" prefix trigger the client-side zip download, so the gateway
// generates ids in that shape. Visibility follows the add-in's own semantics:
// partition_by "account" = owner-only (editable by them), "organization" =
// readable by everyone in the org (admin-managed).
type ExcelSkill struct {
	Id          string `json:"id" gorm:"primaryKey;type:varchar(48)"`
	OrgId       string `json:"org_id" gorm:"type:varchar(64);index:idx_excel_skill_org_name,unique"`
	Name        string `json:"name" gorm:"type:varchar(191);index:idx_excel_skill_org_name,unique"`
	Description string `json:"description" gorm:"type:varchar(1024)"`
	// Instructions is the SKILL.md full text (frontmatter included) — the
	// /instructions endpoint serves it without unzipping.
	Instructions string `json:"instructions" gorm:"type:text"`
	Enabled      bool   `json:"enabled"`
	CreatorType  string `json:"creator_type" gorm:"type:varchar(16);index"`
	IsShared     bool   `json:"is_shared"`
	PartitionBy  string `json:"partition_by" gorm:"type:varchar(16);index"`
	OwnerUserId  int    `json:"owner_user_id" gorm:"index"`
	ZipPath      string `json:"zip_path" gorm:"type:varchar(255)"`
	SizeBytes    int64  `json:"size_bytes"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func (ExcelSkill) TableName() string { return "excel_skills" }

var ErrExcelSkillNotFound = errors.New("excel skill not found")

// CreateExcelSkill inserts a row; duplicate org+name is surfaced as-is (the
// caller turns it into the upload conflict response).
func CreateExcelSkill(s *ExcelSkill) error {
	if DB == nil {
		return nil
	}
	return DB.Create(s).Error
}

// GetExcelSkill resolves one skill scoped to its organization.
func GetExcelSkill(orgId, id string) (*ExcelSkill, error) {
	if id == "" || DB == nil {
		return nil, ErrExcelSkillNotFound
	}
	var s ExcelSkill
	err := DB.Where("id = ? AND org_id = ?", id, orgId).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrExcelSkillNotFound
		}
		return nil, err
	}
	return &s, nil
}

// GetExcelSkillByName resolves the org+name unique row (overwrite target).
func GetExcelSkillByName(orgId, name string) (*ExcelSkill, error) {
	if name == "" || DB == nil {
		return nil, ErrExcelSkillNotFound
	}
	var s ExcelSkill
	err := DB.Where("org_id = ? AND name = ?", orgId, name).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrExcelSkillNotFound
		}
		return nil, err
	}
	return &s, nil
}

// ListExcelSkillsForUser returns the skills an add-in user may see: their own
// account-partitioned uploads plus everything organization-partitioned.
func ListExcelSkillsForUser(orgId string, userId int) ([]*ExcelSkill, error) {
	if DB == nil {
		return nil, nil
	}
	var skills []*ExcelSkill
	err := DB.Where("org_id = ? AND enabled = ? AND (partition_by = ? OR (partition_by = ? AND owner_user_id = ?))",
		orgId, true, "organization", "account", userId).
		Order("created_at desc").Limit(200).Find(&skills).Error
	return skills, err
}

// ListExcelSkillsAll returns every skill in the org for the admin panel.
func ListExcelSkillsAll(orgId string) ([]*ExcelSkill, error) {
	if DB == nil {
		return nil, nil
	}
	var skills []*ExcelSkill
	err := DB.Where("org_id = ?", orgId).Order("created_at desc").Limit(500).Find(&skills).Error
	return skills, err
}

// ExcelSkillNameExists reports whether org+name is taken (upload conflict).
func ExcelSkillNameExists(orgId, name string) (bool, error) {
	if DB == nil {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&ExcelSkill{}).Where("org_id = ? AND name = ?", orgId, name).Count(&cnt).Error
	return cnt > 0, err
}

// ExcelSkillCountForUser counts a user's own uploads (quota check).
func ExcelSkillCountForUser(orgId string, userId int) (int64, error) {
	if DB == nil {
		return 0, nil
	}
	var cnt int64
	err := DB.Model(&ExcelSkill{}).Where("org_id = ? AND owner_user_id = ?", orgId, userId).Count(&cnt).Error
	return cnt, err
}

// UpdateExcelSkill applies a partial update (admin enable/disable, overwrite).
func UpdateExcelSkill(id string, updates map[string]interface{}) error {
	if DB == nil {
		return nil
	}
	return DB.Model(&ExcelSkill{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteExcelSkill removes the row (caller removes the zip from disk).
func DeleteExcelSkill(orgId, id string) error {
	if DB == nil {
		return nil
	}
	return DB.Where("id = ? AND org_id = ?", id, orgId).Delete(&ExcelSkill{}).Error
}
