package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/exceltools"
	"github.com/QuantumNous/new-api/setting/excel_tools"

	"github.com/gin-gonic/gin"
)

// M2 file service handlers — Anthropic Files API compatible subset behind
// /excel/v1/files (TokenAuth). Storage is local disk; see
// docs/excel-files-m2-design.md. Errors use the Anthropic error shape.

func excelFileError(c *gin.Context, status int, typ, message string) {
	c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": typ, "message": message}})
}

// fileViewerResponse is the Anthropic file object shape. The add-in Zod-validates
// the upload response against the full FileMetadata schema, so type / mime_type /
// downloadable are required (mime kept as a legacy alias for older consumers).
func fileViewerResponse(f *model.ExcelFile) gin.H {
	return gin.H{
		"type":         "file",
		"id":           f.Id,
		"filename":     f.Filename,
		"size_bytes":   f.SizeBytes,
		"mime_type":    f.Mime,
		"mime":         f.Mime,
		"purpose":      f.Purpose,
		"status":       "active",
		"created_at":   strconv.FormatInt(f.CreatedAt, 10),
		"expires_at":   strconv.FormatInt(f.ExpiresAt, 10),
		"downloadable": true,
	}
}

// ExcelFileUpload handles POST /excel/v1/files (multipart: file + purpose).
func ExcelFileUpload(c *gin.Context) {
	if !excel_tools.FilesEnabled() {
		excelFileError(c, http.StatusServiceUnavailable, "api_error", "file service is disabled")
		return
	}
	userId := c.GetInt("id")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		// The client only sees the status code; without this line a non-multipart
		// POST (e.g. the add-in's capability probe) is indistinguishable from a
		// quota rejection in the logs (observed live: sub-ms 400s with no cause).
		common.SysLog(fmt.Sprintf("[excel-files] upload rejected: not multipart or missing field: content_type=%s content_length=%d remote=%s err=%v",
			c.ContentType(), c.Request.ContentLength, c.ClientIP(), err))
		excelFileError(c, http.StatusBadRequest, "invalid_request_error", "multipart field 'file' is required")
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		excelFileError(c, http.StatusBadRequest, "invalid_request_error", "cannot open uploaded file")
		return
	}
	defer src.Close()

	row, err := exceltools.SaveExcelFile(userId, fileHeader.Filename, c.PostForm("purpose"), fileHeader.Size, src)
	if err != nil {
		common.SysLog(fmt.Sprintf("[excel-files] upload rejected: user=%d filename=%q size=%d err=%v",
			userId, fileHeader.Filename, fileHeader.Size, err))
		excelFileError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	c.JSON(http.StatusOK, fileViewerResponse(row))
}

// ExcelFileList handles GET /excel/v1/files.
func ExcelFileList(c *gin.Context) {
	if !excel_tools.FilesEnabled() {
		excelFileError(c, http.StatusServiceUnavailable, "api_error", "file service is disabled")
		return
	}
	files, err := model.ListExcelFiles(c.GetInt("id"))
	if err != nil {
		excelFileError(c, http.StatusInternalServerError, "api_error", "list failed")
		return
	}
	data := make([]gin.H, 0, len(files))
	for _, f := range files {
		data = append(data, fileViewerResponse(f))
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "has_more": false, "next_page": nil})
}

// ExcelFileDownload handles GET /excel/v1/files/:id — owner-scoped streaming.
func ExcelFileDownload(c *gin.Context) {
	if !excel_tools.FilesEnabled() {
		excelFileError(c, http.StatusServiceUnavailable, "api_error", "file service is disabled")
		return
	}
	row, body, err := exceltools.OpenExcelFile(c.Param("id"), c.GetInt("id"))
	if err != nil {
		// 404 (not 403) so existence of other users' files is not leaked.
		excelFileError(c, http.StatusNotFound, "not_found_error", "file not found")
		return
	}
	defer body.Close()
	c.Header("Content-Disposition", `attachment; filename="`+row.Filename+`"`)
	c.DataFromReader(http.StatusOK, row.SizeBytes, row.Mime, body, nil)
}

// ExcelFileDelete handles DELETE /excel/v1/files/:id.
func ExcelFileDelete(c *gin.Context) {
	if !excel_tools.FilesEnabled() {
		excelFileError(c, http.StatusServiceUnavailable, "api_error", "file service is disabled")
		return
	}
	id := c.Param("id")
	if err := exceltools.DeleteExcelFile(id, c.GetInt("id")); err != nil {
		excelFileError(c, http.StatusNotFound, "not_found_error", "file not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "deleted": true})
}
