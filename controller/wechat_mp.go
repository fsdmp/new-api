package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/wechat_mp"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// mpQRExpireSeconds 公众号扫码登录二维码过期时间（秒）。微信临时二维码最长 120s。
const mpQRExpireSeconds = 120

// mpSceneIdPrefix 为生成的 sceneId 加上前缀，便于辨识并避免与其它扫码场景冲突。
const mpSceneIdPrefix = "sl_"

// qrcodeRequest 前端请求生成二维码时携带的参数
type qrcodeRequest struct {
	AffCode string `json:"aff_code"`
}

// WeChatMpQrCode 生成一个公众号扫码登录用的临时二维码。
// 流程：
//  1. 校验管理员是否开启扫码登录
//  2. 生成 scene_id（32 字符随机串，前缀 sl_）
//  3. Redis 写入 pending 状态（TTL=180s）
//  4. 调用微信接口创建临时二维码（expire=120s）
//  5. 返回 scene_id + qr_url + expire_seconds
func WeChatMpQrCode(c *gin.Context) {
	settings := system_setting.GetWeChatOAuthSettings()
	if !settings.ScanLoginEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启微信扫码登录",
		})
		return
	}
	if !common.RedisEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "微信扫码登录需要启用 Redis",
		})
		return
	}
	if settings.AppId == "" || settings.AppSecret == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未配置微信公众号 AppId/AppSecret",
		})
		return
	}

	var req qrcodeRequest
	// body 可选：允许为空
	_ = common.DecodeJson(c.Request.Body, &req)

	sceneId := mpSceneIdPrefix + common.GetRandomString(32)
	if err := wechat_mp.CreateMpSession(sceneId, req.AffCode); err != nil {
		common.SysLog("create wechat mp session failed: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "创建扫码会话失败",
		})
		return
	}

	accessToken, err := wechat_mp.GetAccessToken(settings.AppId, settings.AppSecret)
	if err != nil {
		common.SysLog("get wechat access_token failed: " + err.Error())
		wechat_mp.DeleteMpSession(sceneId)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取公众号 access_token 失败",
		})
		return
	}

	_, qrUrl, err := wechat_mp.CreateTempQRCode(accessToken, sceneId, mpQRExpireSeconds)
	if err != nil {
		common.SysLog("create wechat qrcode failed: " + err.Error())
		wechat_mp.DeleteMpSession(sceneId)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "创建二维码失败：" + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"scene_id":       sceneId,
			"qr_url":         qrUrl,
			"expire_seconds": mpQRExpireSeconds,
		},
	})
}

// WeChatMpStatus 轮询公众号扫码登录状态。
// - pending: 等待扫码
// - scanned: 已扫码（理论上不会向客户端返回此状态，会立即完成登录）
// - logged_in: 扫码后一次性返回用户信息
// - expired: 会话已过期
//
// 当检测到 scanned 时，后端会立即完成用户查找/创建、setupLogin、推送客服消息，
// 然后返回 logged_in 状态及用户数据。前端拿到此响应后即可跳转。
func WeChatMpStatus(c *gin.Context) {
	settings := system_setting.GetWeChatOAuthSettings()
	if !settings.ScanLoginEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启微信扫码登录",
			"data":    gin.H{"status": "failed"},
		})
		return
	}

	sceneId := c.Query("scene_id")
	if sceneId == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少 scene_id",
		})
		return
	}

	state, err := wechat_mp.GetMpSession(sceneId)
	if err != nil {
		// Redis 中没有找到（可能已过期或已登录）
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "二维码已过期",
			"data":    gin.H{"status": "expired"},
		})
		return
	}

	switch state.Status {
	case wechat_mp.StatusPending:
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"status":   "pending",
				"scene_id": sceneId,
			},
		})
		return
	case wechat_mp.StatusScanned:
		// 扫码成功，立即完成登录
		user, loginErr := findOrCreateWeChatMpUser(state.OpenID, state.Nickname, state.AffCode)
		if loginErr != nil {
			// 登录失败，清理会话
			_ = markMpFailed(sceneId)
			common.SysLog("wechat mp login failed: " + loginErr.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": loginErr.Error(),
				"data":    gin.H{"status": "failed"},
			})
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "用户已被封禁",
				"data":    gin.H{"status": "failed"},
			})
			return
		}

		// 标记 logged_in 并立即删除 Redis 键（防重放）
		if _, err := wechat_mp.MarkLoggedIn(sceneId, user.Id); err != nil {
			// 标记失败（可能已被其他并发请求处理），但用户已经拿到，继续完成登录
			common.SysLog("mark wechat mp logged_in failed: " + err.Error())
		}

		// 异步推送一条公众号客服消息给用户（登录成功提示），失败不影响登录流程
		go pushMpLoginSuccessMessage(settings, state.OpenID, user)

		// 完成登录：写 session + 返回用户数据。
		// 注意：不能直接复用 setupLogin，因为它把 data.status 设为 user.Status（int），
		// 与前端状态机期待的 data.status="logged_in"（string）冲突。这里使用专用版本。
		setupMpLogin(user, sceneId, c)
		return
	case wechat_mp.StatusLoggedIn:
		// 极少数情况：状态还是 logged_in（理论上 MarkLoggedIn 后已删除）
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "登录会话已处理，请重新生成二维码",
			"data":    gin.H{"status": "expired"},
		})
		return
	case wechat_mp.StatusFailed:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "登录失败，请重试",
			"data":    gin.H{"status": "failed"},
		})
		return
	default:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "未知状态",
			"data":    gin.H{"status": "failed"},
		})
		return
	}
}

// WeChatMpCallback 公众号服务器配置回调入口。
// GET  → 微信服务器签名校验（返回 echostr）
// POST → 接收事件推送 XML
//
// 此端点不挂 CriticalRateLimit，因为它是微信服务器调用。
func WeChatMpCallback(c *gin.Context) {
	settings := system_setting.GetWeChatOAuthSettings()
	if !settings.ScanLoginEnabled {
		// 即便关闭了扫码登录，也返回 200 防止微信反复重试
		c.String(http.StatusOK, "")
		return
	}

	signature := c.Query("signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	echostr := c.Query("echostr")

	if !wechat_mp.VerifySignature(settings.Token, timestamp, nonce, signature) {
		common.SysLog("wechat mp callback signature mismatch")
		c.String(http.StatusForbidden, "signature mismatch")
		return
	}

	if c.Request.Method == http.MethodGet {
		// 微信首次验证服务器地址
		c.String(http.StatusOK, echostr)
		return
	}

	// POST：解析 XML 事件
	msg, err := wechat_mp.ParseMessage(c.Request.Body)
	if err != nil {
		common.SysLog("parse wechat callback xml failed: " + err.Error())
		c.String(http.StatusOK, "")
		return
	}

	// 仅关注 subscribe/SCAN 事件
	sceneId := wechat_mp.ExtractSceneId(msg)
	if sceneId == "" {
		// 非扫码事件，直接返回空
		c.String(http.StatusOK, "")
		return
	}

	// 先尝试从微信拉取昵称（失败不阻断流程）
	nickname := ""
	if accessToken, err := wechat_mp.GetAccessToken(settings.AppId, settings.AppSecret); err == nil {
		if info, err := wechat_mp.GetUserInfo(accessToken, msg.FromUserName); err == nil && info.Nickname != "" {
			nickname = info.Nickname
		}
	}

	ok, err := wechat_mp.HandleScanEvent(sceneId, msg.FromUserName, nickname)
	if err != nil {
		common.SysLog("handle wechat mp event failed: " + err.Error())
		c.String(http.StatusOK, "")
		return
	}

	var reply string
	if ok {
		reply = "扫码成功，请返回网页完成登录。"
	} else {
		// 重复扫码或 sceneId 已过期
		reply = "二维码已失效，请刷新后重新扫码。"
	}
	// 注意：回复消息的 ToUserName 是发送方（用户 openid），FromUserName 是公众号
	xmlReply := wechat_mp.ReplyTextXML(msg.FromUserName, msg.ToUserName, reply)
	// 直接写入 XML 字符串（微信对 Content-Type 不敏感）
	c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(xmlReply))
}

// findOrCreateWeChatMpUser 找到或创建一个公众号扫码登录用户。
// 与现有 controller/wechat.go::WeChatAuth 保持相同行为：
//   - 若 openid 已存在用户 → 返回该用户
//   - 否则在 RegisterEnabled 时新建用户
func findOrCreateWeChatMpUser(openID, nickname, affCode string) (*model.User, error) {
	if openID == "" {
		return nil, fmt.Errorf("openid 为空")
	}
	user := &model.User{WeChatId: openID}
	if model.IsWeChatIdAlreadyTaken(openID) {
		if err := user.FillUserByWeChatId(); err != nil {
			return nil, err
		}
		if user.Id == 0 {
			return nil, fmt.Errorf("用户已注销")
		}
		return user, nil
	}

	// 新建用户
	if !common.RegisterEnabled {
		return nil, fmt.Errorf("管理员关闭了新用户注册")
	}

	user.Username = "wechat_mp_" + strconv.Itoa(model.GetMaxUserId()+1)
	if nickname == "" {
		nickname = "WeChat User"
	}
	user.DisplayName = nickname
	user.Role = common.RoleCommonUser
	user.Status = common.UserStatusEnabled

	inviterId := 0
	if affCode != "" {
		if id, err := model.GetUserIdByAffCode(affCode); err == nil && id > 0 {
			inviterId = id
		}
	}

	if err := user.Insert(inviterId); err != nil {
		return nil, err
	}
	user.FinalizeOAuthUserCreation(inviterId)
	return user, nil
}

// markMpFailed 把会话标记为 failed（用于登录失败时）
func markMpFailed(sceneId string) error {
	// 直接删除即可，防止客户端持续轮询到一个错误状态
	wechat_mp.DeleteMpSession(sceneId)
	return nil
}

// pushMpLoginSuccessMessage 异步向用户推送一条客服消息（登录成功文案）
// 失败仅记录日志，不影响主流程。
func pushMpLoginSuccessMessage(settings *system_setting.WeChatOAuthSettings, openID string, user *model.User) {
	defer func() {
		if r := recover(); r != nil {
			common.SysLog(fmt.Sprintf("push mp login message panic: %v", r))
		}
	}()

	accessToken, err := wechat_mp.GetAccessToken(settings.AppId, settings.AppSecret)
	if err != nil {
		common.SysLog("push mp login message: get access_token failed: " + err.Error())
		return
	}

	content := fmt.Sprintf("登录成功！欢迎 %s，您已通过微信扫码安全登录。", user.DisplayName)
	// 客服消息有 48 小时窗口限制，扫码事件本身就在窗口内，可直接发送
	if err := wechat_mp.SendCustomTextMessage(accessToken, openID, content); err != nil {
		common.SysLog("push mp login message failed: " + err.Error())
	}
}

// setupMpLogin 是 setupLogin 的变体，专为公众号扫码登录场景定制。
// 与 setupLogin 的差异：
//   - data.status 设为字符串 "logged_in"（前端状态机识别用），而非 user.Status（int）
//   - 用户启用态改放在 data.user_status
//   - 额外返回 data.scene_id，便于客户端对账
//
// 其余字段（id/username/display_name/role/group/access_token）与 setupLogin 完全一致，
// 保证非 cookie 客户端可以直接拿 access_token + id 作为长期凭证。
func setupMpLogin(user *model.User, sceneId string, c *gin.Context) {
	model.UpdateUserLastLoginAt(user.Id)

	// 若用户还没有 access_token，则生成并持久化（供非 cookie 客户端使用）
	if user.GetAccessToken() == "" {
		randI := common.GetRandomInt(4)
		key, err := common.GenerateRandomKey(29 + randI)
		if err != nil {
			common.SysLog("failed to generate access token: " + err.Error())
		} else {
			user.SetAccessToken(key)
			if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("access_token", key).Error; err != nil {
				common.SysLog("failed to save access token: " + err.Error())
			}
		}
	}

	session := sessions.Default(c)
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("group", user.Group)
	if err := session.Save(); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data": map[string]any{
			"status":       "logged_in", // 状态机标识（string），与 pending/expired/failed 同维
			"scene_id":     sceneId,
			"id":           user.Id,
			"username":     user.Username,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"user_status":  user.Status, // 用户启用态（int），对应 common.UserStatusEnabled 等
			"group":        user.Group,
			"access_token": user.GetAccessToken(),
		},
	})
}
