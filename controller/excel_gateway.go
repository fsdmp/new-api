package controller

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/excel_setting"
	"github.com/gin-gonic/gin"
)

func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func MetricsMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"partialSuccess": gin.H{}})
}

func TracesMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"partialSuccess": gin.H{}})
}

func EvalMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"features": gin.H{}})
}

func VersionMock(c *gin.Context) {
	sha := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				sha = s.Value
				break
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"sha": sha})
}

func AnalyticsMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func ShortcutsMock(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// ExcelListModels returns the list of models in the Anthropic-style
// paginated format used by cc-excel-server-demo's /v1/models endpoint.
// Models are served from the configured alias list (env vars), with
// display names and id mapping matching the demo behaviour.
func ExcelListModels(c *gin.Context) {
	aliases := excel_setting.GetExcelModelAliases()
	now := time.Now().UTC().Format(time.RFC3339)

	anthropicModels := make([]dto.AnthropicModel, 0, len(aliases))
	for _, alias := range aliases {
		anthropicModels = append(anthropicModels, dto.AnthropicModel{
			ID:          alias.ID,
			CreatedAt:   now,
			DisplayName: alias.DisplayName,
			Type:        "model",
		})
	}

	if len(anthropicModels) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"data":     []dto.AnthropicModel{},
			"has_more": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":     anthropicModels,
		"first_id": anthropicModels[0].ID,
		"last_id":  anthropicModels[len(anthropicModels)-1].ID,
		"has_more": false,
	})
}
