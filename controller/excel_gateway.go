package controller

import (
	"errors"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/excel_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

type excelTmpKeyRequest struct {
	DeviceID string `json:"device_id"`
}

// CreateExcelTmpKey creates (or returns an existing) temporary API key
// for the requesting device. No authentication is required.
func CreateExcelTmpKey(c *gin.Context) {
	if !excel_setting.GetExcelTmpKeyEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "temporary key service is not enabled",
		})
		return
	}

	account := excel_setting.GetExcelTmpAccount()
	if account == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "temporary key service is not configured",
		})
		return
	}

	var req excelTmpKeyRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.DeviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "device_id is required",
		})
		return
	}
	if len(req.DeviceID) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "device_id must be at most 128 characters",
		})
		return
	}

	user, err := model.GetUserByUsername(account)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "failed to lookup public account",
		})
		return
	}
	if user == nil || user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "public account is not available",
		})
		return
	}

	tokenName := "aiexceltmp-" + req.DeviceID
	existToken, err := model.GetTokenByUserIdAndName(user.Id, tokenName)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to query existing token",
		})
		return
	}

	if existToken != nil {
		// Token exists and not expired — return it
		if existToken.ExpiredTime == -1 || existToken.ExpiredTime > time.Now().Unix() {
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"key":        existToken.GetFullKey(),
				"expires_at": existToken.ExpiredTime,
			})
			return
		}
		// Expired — soft-delete, then create new
		_ = existToken.Delete()
	}

	expireDays := excel_setting.GetExcelTmpKeyExpireDays()
	quota := excel_setting.GetExcelTmpKeyQuota()
	key, err := common.GenerateKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to generate key",
		})
		return
	}

	expiredTime := time.Now().Add(time.Duration(expireDays) * 24 * time.Hour).Unix()
	token := &model.Token{
		UserId:         user.Id,
		Key:            key,
		Name:           tokenName,
		CreatedTime:    common.GetTimestamp(),
		ExpiredTime:    expiredTime,
		RemainQuota:    quota,
		UnlimitedQuota: false,
		Status:         common.TokenStatusEnabled,
	}

	if err := token.Insert(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to create token",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"key":        token.GetFullKey(),
		"expires_at": expiredTime,
	})
}

// CheckVersion checks whether the client's version meets the configured
// minimum for the requested version type (e.g. "excel-plugin", "ai-sdk").
//
// Query params: version (required), type (required).
func CheckVersion(c *gin.Context) {
	version := c.Query("version")
	versionType := c.Query("type")

	if version == "" || versionType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "version and type query parameters are required",
		})
		return
	}

	minimumVersion, found := excel_setting.GetMinimumVersion(versionType)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "unknown version type",
		})
		return
	}

	meetsMinimum, err := common.ParseAndCompareSemVer(version, minimumVersion)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid version format",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"meets_minimum":   meetsMinimum,
			"minimum_version": minimumVersion,
			"version":         version,
			"type":            versionType,
		},
	})
}
