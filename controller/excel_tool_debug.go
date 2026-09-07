package controller

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/exceltools"
	"github.com/QuantumNous/new-api/setting/excel_tools"

	"github.com/gin-gonic/gin"
)

// ExcelToolDebug serves POST /excel/api/tools/{web_search|web_fetch|
// code_execution} — direct executor invocation for联调/压测. Flag-gated
// (excel_tools.debug_endpoints, default false); behind TokenAuth.
func ExcelToolDebug(c *gin.Context) {
	if !excel_tools.DebugEndpointsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found", "message": "no such endpoint"}})
		return
	}
	tool := c.Param("tool")
	if exceltools.LookupExecutor(tool) == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found", "message": "unknown tool: " + tool}})
		return
	}

	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "read body failed"}})
		return
	}
	var input map[string]interface{}
	if len(raw) > 0 {
		if err := common.Unmarshal(raw, &input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "body must be a JSON object"}})
			return
		}
	}
	model := ""
	if m, ok := input["model"].(string); ok {
		model = m
	}

	res := exceltools.ExecuteDebugCall(c, tool, model, input)
	resp := gin.H{
		"tool":        tool,
		"status":      res.StatusShort,
		"duration_ms": res.DurationMs,
		"model_text":  res.ModelText,
	}
	if res.Block != nil {
		resp["block"] = res.Block
	}
	if res.Err != nil {
		resp["error"] = gin.H{"code": res.Err.Code, "message": res.Err.Message}
	}
	c.JSON(http.StatusOK, resp)
}
