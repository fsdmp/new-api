package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// requireTosAcceptance checks if the user has accepted the current ToS version.
// Returns true if the check passes (user can proceed).
// Returns false if the user needs to accept (sends error response).
// Skips the check if no ToS content is configured.
func requireTosAcceptance(c *gin.Context) bool {
	legalSetting := system_setting.GetLegalSettings()
	if legalSetting.TermsOfService == "" {
		return true
	}

	tosSetting := system_setting.GetTosSettings()
	userId := c.GetInt("id")
	if userId == 0 {
		return true
	}

	var user model.User
	if err := model.DB.Select("tos_accepted_version").First(&user, userId).Error; err != nil {
		return true
	}

	if user.TosAcceptedVersion == tosSetting.TosVersion {
		return true
	}

	common.ApiErrorI18n(c, i18n.MsgTosAcceptanceRequired)
	return false
}

func GetTosStatus(c *gin.Context) {
	userId := c.GetInt("id")
	needsAcceptance := false

	legalSetting := system_setting.GetLegalSettings()
	if legalSetting.TermsOfService != "" && userId > 0 {
		tosSetting := system_setting.GetTosSettings()
		var user model.User
		if err := model.DB.Select("tos_accepted_version").First(&user, userId).Error; err == nil {
			needsAcceptance = user.TosAcceptedVersion != tosSetting.TosVersion
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"needs_acceptance": needsAcceptance,
		},
	})
}

func AcceptTos(c *gin.Context) {
	userId := c.GetInt("id")
	if userId == 0 {
		common.ApiErrorI18n(c, i18n.MsgAuthNotLoggedIn)
		return
	}

	tosSetting := system_setting.GetTosSettings()
	now := time.Now().Unix()
	clientIP := c.ClientIP()

	err := model.DB.Model(&model.User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"tos_accepted_version": tosSetting.TosVersion,
		"tos_accepted_at":      now,
		"tos_accepted_ip":      clientIP,
	}).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"accepted":    true,
			"version":     tosSetting.TosVersion,
			"accepted_at": now,
			"accepted_ip": clientIP,
		},
	})
}
