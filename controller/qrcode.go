package controller

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/wechat_mp"

	"github.com/gin-gonic/gin"
)

// qrCodeNameKeys 二维码业务名 -> option key
var qrCodeNameKeys = map[string]string{
	"mp_group": "MPGroupQRCode",
	"site":     "SiteQRCode",
}

// qrCodeAllowedFormats 允许的图片格式 -> 上传给微信时的文件扩展名
// （微信客服消息图片仅支持 jpg/png）
var qrCodeAllowedFormats = map[string]string{
	"jpeg": ".jpg",
	"png":  ".png",
}

const (
	qrCodeMaxImageBytes = 2 << 20 // 解码后 2MB
	qrCodeMaxDimension  = 4000    // 最长边像素
)

func getQRCodeOptionValue(name string) string {
	key, ok := qrCodeNameKeys[name]
	if !ok {
		return ""
	}
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[key]
}

// GetCustomQRCodeBytes 返回管理端上传的自定义二维码（解码字节 + 格式名）。
// 未上传或数据损坏返回 nil，调用方走各自回退。供本文件与 wechat_mp.go 共用。
func GetCustomQRCodeBytes(name string) ([]byte, string) {
	dataURL := getQRCodeOptionValue(name)
	if dataURL == "" {
		return nil, ""
	}
	_, format, cleanBase64, err := service.DecodeBase64ImageData(dataURL)
	if err != nil {
		common.SysLog("qrcode[" + name + "]: decode custom image failed: " + err.Error())
		return nil, ""
	}
	if _, ok := qrCodeAllowedFormats[format]; !ok {
		return nil, ""
	}
	data, err := base64.StdEncoding.DecodeString(cleanBase64)
	if err != nil || len(data) == 0 {
		return nil, ""
	}
	return data, format
}

// GetQRCodeImage GET /api/qrcode/:name（公开）
// mp_group：自定义优先，未上传回退内嵌图；site：未上传 404
func GetQRCodeImage(c *gin.Context) {
	name := c.Param("name")
	if _, ok := qrCodeNameKeys[name]; !ok {
		c.String(http.StatusNotFound, "unknown qrcode")
		return
	}
	if data, format := GetCustomQRCodeBytes(name); data != nil {
		c.Header("Cache-Control", "public, max-age=60")
		c.Data(http.StatusOK, "image/"+format, data)
		return
	}
	if name == "mp_group" {
		c.Header("Cache-Control", "public, max-age=60")
		c.Data(http.StatusOK, "image/jpeg", wechat_mp.GroupQRCodeImage())
		return
	}
	c.String(http.StatusNotFound, "qrcode not uploaded")
}

// GetQRCodeStatus GET /api/qrcode/status（root）— 管理端判断是否已上传自定义图
func GetQRCodeStatus(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"mp_group": strings.TrimSpace(common.OptionMap["MPGroupQRCode"]) != "",
			"site":     strings.TrimSpace(common.OptionMap["SiteQRCode"]) != "",
		},
	})
}

type qrcodeUploadRequest struct {
	Image string `json:"image"` // data:image/jpeg;base64,... 或纯 base64
}

// UploadQRCode PUT /api/qrcode/:name（root）
func UploadQRCode(c *gin.Context) {
	name := c.Param("name")
	key, ok := qrCodeNameKeys[name]
	if !ok {
		common.ApiErrorMsg(c, "未知的二维码类型")
		return
	}
	var req qrcodeUploadRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Image == "" {
		common.ApiErrorMsg(c, "无效的请求体")
		return
	}
	cfg, format, cleanBase64, err := service.DecodeBase64ImageData(req.Image)
	if err != nil {
		common.ApiErrorMsg(c, "图片数据无效，仅支持 JPG/PNG")
		return
	}
	if _, allowed := qrCodeAllowedFormats[format]; !allowed {
		common.ApiErrorMsg(c, "仅支持 JPG/PNG 格式图片")
		return
	}
	if cfg.Width > qrCodeMaxDimension || cfg.Height > qrCodeMaxDimension {
		common.ApiErrorMsg(c, "图片尺寸过大，最长边不能超过 4000px")
		return
	}
	data, err := base64.StdEncoding.DecodeString(cleanBase64)
	if err != nil || len(data) == 0 {
		common.ApiErrorMsg(c, "图片数据解码失败")
		return
	}
	if len(data) > qrCodeMaxImageBytes {
		common.ApiErrorMsg(c, "图片不能超过 2MB")
		return
	}
	// 规范化为完整 data URL 存储，读取端统一处理
	if err := model.UpdateOption(key, "data:image/"+format+";base64,"+cleanBase64); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// DeleteQRCode DELETE /api/qrcode/:name（root）— 置空即回退默认
func DeleteQRCode(c *gin.Context) {
	name := c.Param("name")
	key, ok := qrCodeNameKeys[name]
	if !ok {
		common.ApiErrorMsg(c, "未知的二维码类型")
		return
	}
	if err := model.UpdateOption(key, ""); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
