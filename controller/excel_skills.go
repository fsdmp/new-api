package controller

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/exceltools"

	"github.com/gin-gonic/gin"
)

// M5 skill cloud sync (docs/excel-mcp-skills-m4m5-design.md §2.4). The
// add-in hardcodes the claude.ai-style paths below (the overlay leaves
// /api/oauth/* unprefixed), so these live at the domain root, TokenAuth'd by
// the same sk- token as every other Excel-gateway call.

// excelSkillOrgConstant is the organization UUID baked into the add-in build
// (VITE_INTERNAL_ORG_UUID). All skills live under it for now.
const excelSkillOrgConstant = "ccexcel"

// skillError writes the anthropic-style error envelope used across the
// Excel gateway (same shape as /excel/v1/files).
func skillError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": errType, "message": message}})
}

// skillViewerResponse renders the list-skills item shape the add-in expects.
// creator_type/is_shared/partition_by mirror O3()'s editable-skill test so
// account-partitioned skills are editable in the UI.
func skillViewerResponse(s *model.ExcelSkill) gin.H {
	return gin.H{
		"id":          s.Id,
		"name":        s.Name,
		"description": s.Description,
		"enabled":     s.Enabled,
		"source":      "remote",
		"creator_type": s.CreatorType,
		"is_shared":    s.IsShared,
		"partition_by": s.PartitionBy,
		"size_bytes":   s.SizeBytes,
		"created_at":   s.CreatedAt,
		"updated_at":   s.UpdatedAt,
	}
}

// ExcelSkillList serves GET /api/oauth/organizations/:org/skills/list-skills.
// The add-in accepts a bare array, {data:[...]} or {skills:[...]} — we send a
// bare array.
func ExcelSkillList(c *gin.Context) {
	org := c.Param("org")
	if org == "" || org != excelSkillOrgConstant {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}
	skills, err := model.ListExcelSkillsForUser(org, c.GetInt("id"))
	if err != nil {
		skillError(c, http.StatusInternalServerError, "api_error", "list skills failed")
		return
	}
	out := make([]gin.H, 0, len(skills))
	for _, s := range skills {
		out = append(out, skillViewerResponse(s))
	}
	c.JSON(http.StatusOK, out)
}

// ExcelSkillInstructions serves GET .../skills/:skill_id/instructions — the
// read_skill tool's data source (add-in times out after 5s; response is tiny).
func ExcelSkillInstructions(c *gin.Context) {
	org := c.Param("org")
	s, err := model.GetExcelSkill(org, c.Param("skill_id"))
	if err != nil {
		skillError(c, http.StatusNotFound, "not_found", "skill not found")
		return
	}
	// account-partitioned skills are owner-only
	if s.PartitionBy == "account" && s.OwnerUserId != c.GetInt("id") {
		skillError(c, http.StatusNotFound, "not_found", "skill not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"instructions": s.Instructions})
}

// ExcelSkillDownload serves GET .../skills/:skill_id/download — streams the
// stored zip (the add-in unzips client-side and reads SKILL.md).
func ExcelSkillDownload(c *gin.Context) {
	org := c.Param("org")
	s, err := model.GetExcelSkill(org, c.Param("skill_id"))
	if err != nil {
		skillError(c, http.StatusNotFound, "not_found", "skill not found")
		return
	}
	if s.PartitionBy == "account" && s.OwnerUserId != c.GetInt("id") {
		skillError(c, http.StatusNotFound, "not_found", "skill not found")
		return
	}
	f, err := exceltools.OpenExcelSkillZip(s)
	if err != nil {
		if os.IsNotExist(err) {
			skillError(c, http.StatusNotFound, "not_found", "skill body missing")
			return
		}
		skillError(c, http.StatusInternalServerError, "api_error", "open skill body failed")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		skillError(c, http.StatusInternalServerError, "api_error", "stat skill body failed")
		return
	}
	c.DataFromReader(http.StatusOK, fi.Size(), "application/zip", f, map[string]string{
		"Content-Disposition": `attachment; filename="` + s.Name + `.skill"`,
	})
}

// ExcelSkillUpload serves POST .../skills/upload-skill — multipart file field
// (named <name>.skill by the add-in, but any name is accepted), optional
// overwrite=true + check_skill_name query params.
func ExcelSkillUpload(c *gin.Context) {
	org := c.Param("org")
	if org != excelSkillOrgConstant {
		skillError(c, http.StatusBadRequest, "invalid_request", "unknown organization")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		skillError(c, http.StatusBadRequest, "invalid_request", "multipart file field is required")
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		skillError(c, http.StatusBadRequest, "invalid_request", "open upload failed")
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(exceltools.MaxSkillZipBytes())+1))
	if err != nil {
		skillError(c, http.StatusBadRequest, "invalid_request", "read upload failed")
		return
	}
	if len(raw) > exceltools.MaxSkillZipBytes() {
		skillError(c, http.StatusRequestEntityTooLarge, "request_too_large", "skill zip exceeds size limit")
		return
	}

	overwrite := c.Query("overwrite") == "true"
	fallbackName := strings.TrimSuffix(fileHeader.Filename, ".skill")
	fallbackName = strings.TrimSuffix(fallbackName, filepath.Ext(fallbackName))
	if q := c.Query("check_skill_name"); q != "" {
		fallbackName = q
	}

	parsed, err := exceltools.ParseSkillZip(raw, fallbackName)
	if err != nil {
		skillError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// quota: own uploads
	cnt, _ := model.ExcelSkillCountForUser(org, c.GetInt("id"))
	if cnt >= int64(exceltools.MaxSkillsPerUser()) && !overwrite {
		skillError(c, http.StatusForbidden, "quota_exceeded", "技能数量已达上限")
		return
	}

	row, err := exceltools.SaveExcelSkill(org, c.GetInt("id"), parsed, "account", overwrite)
	if err == exceltools.ErrSkillNameConflict {
		skillError(c, http.StatusConflict, "name_conflict", "同名技能已存在，可携带 overwrite=true 覆盖")
		return
	}
	if err != nil {
		skillError(c, http.StatusInternalServerError, "api_error", "save skill failed")
		return
	}
	c.JSON(http.StatusOK, skillViewerResponse(row))
}

// ---- admin endpoints (session-authed, classic admin panel) ----

// ExcelSkillAdminList serves GET /api/excel/skills — all skills in the org.
func ExcelSkillAdminList(c *gin.Context) {
	skills, err := model.ListExcelSkillsAll(excelSkillOrgConstant)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(skills))
	for _, s := range skills {
		out = append(out, skillViewerResponse(s))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// ExcelSkillAdminUpload serves POST /api/excel/skills — admin upload; the
// visibility query/body field selects partition_by (default organization).
func ExcelSkillAdminUpload(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请选择 zip 文件"})
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "读取文件失败"})
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(exceltools.MaxSkillZipBytes())+1))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "读取文件失败"})
		return
	}
	if len(raw) > exceltools.MaxSkillZipBytes() {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "zip 超过 10MB 上限"})
		return
	}
	overwrite := c.PostForm("overwrite") == "true" || c.Query("overwrite") == "true"
	fallbackName := strings.TrimSuffix(fileHeader.Filename, ".skill")
	if q := c.Query("check_skill_name"); q != "" {
		fallbackName = q
	}
	parsed, err := exceltools.ParseSkillZip(raw, fallbackName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	partition := c.DefaultPostForm("partition_by", "organization")
	row, err := exceltools.SaveExcelSkill(excelSkillOrgConstant, c.GetInt("id"), parsed, partition, overwrite)
	if err == exceltools.ErrSkillNameConflict {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "同名技能已存在，勾选覆盖后重试"})
		return
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "保存失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": skillViewerResponse(row)})
}

// ExcelSkillAdminUpdate serves PUT /api/excel/skills/:id — enable/disable or
// switch visibility.
func ExcelSkillAdminUpdate(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Enabled     *bool  `json:"enabled"`
		PartitionBy string `json:"partition_by"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid body"})
		return
	}
	updates := map[string]interface{}{"updated_at": common.GetTimestamp()}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.PartitionBy == "organization" || req.PartitionBy == "account" {
		updates["partition_by"] = req.PartitionBy
	}
	if err := model.UpdateExcelSkill(id, updates); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ExcelSkillAdminDelete serves DELETE /api/excel/skills/:id.
func ExcelSkillAdminDelete(c *gin.Context) {
	id := c.Param("id")
	if _, err := model.GetExcelSkill(excelSkillOrgConstant, id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "技能不存在"})
		return
	}
	if err := model.DeleteExcelSkill(excelSkillOrgConstant, id); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	exceltools.DeleteExcelSkillBody(id)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
