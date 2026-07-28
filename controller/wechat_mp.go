package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

// mpSubscribeReply 普通关注（非扫码登录场景）时的欢迎文案。
// 注意：扫码登录触发的 subscribe 事件 EventKey 带 "qrscene_" 前缀，
// ExtractSceneId 会返回真实的 sceneId，不会走到使用此文案的分支。
const mpSubscribeReply = `哈喽～很高兴被你收留啦🍵
我是专属于你的 AI 温柔办公小助理
平日里纠结的工作总结、策划文案、发言稿
润色文字、梳理大纲、整理表格思绪
大大小小的办公烦恼，都可以讲给我听～
不想被工作裹挟、不想熬夜加班的日子
就让我替你分担琐碎忙碌吧`

// mpDefaultReply 非关注、非扫码消息（文本/图片/菜单点击等）的默认回复。
const mpDefaultReply = "好哒，已经记下你的需求咯☕"

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

	// 仅关注 subscribe/SCAN 事件用于扫码登录
	sceneId := wechat_mp.ExtractSceneId(msg)
	if sceneId == "" {
		// 非扫码事件：根据事件类型回复对应文案
		// —— 服务器配置启用后，公众号后台的「被关注回复」与「自动回复」会被接管失效，
		//    这里负责把这两个核心场景补回来。
		var reply string
		if msg.Event == "subscribe" {
			// 普通关注（EventKey 不带 qrscene_ 前缀）
			reply = mpSubscribeReply
		} else {
			// 其他消息：文本 / 图片 / 语音 / 菜单点击（CLICK）/ 位置等
			reply = mpDefaultReply
		}
		xmlReply := wechat_mp.ReplyTextXML(msg.FromUserName, msg.ToUserName, reply)
		c.Data(http.StatusOK, "application/xml; charset=utf-8", []byte(xmlReply))
		return
	}

	// 先尝试从微信拉取昵称（失败不阻断流程）
	nickname := ""
	if accessToken, err := wechat_mp.GetAccessToken(settings.AppId, settings.AppSecret); err == nil {
		if info, err := wechat_mp.GetUserInfo(accessToken, msg.FromUserName); err == nil && info.Nickname != "" {
			nickname = info.Nickname
		} else if err != nil {
			// 常见原因：订阅号未认证 → 48001 api unauthorized；没有「用户管理」接口权限
			common.SysLog(fmt.Sprintf("wechat mp fetch nickname failed (openid=%s): %s", msg.FromUserName, err.Error()))
		} else {
			// 接口调通了但 nickname 为空（用户设置了空昵称）
			common.SysLog(fmt.Sprintf("wechat mp user info returned empty nickname (openid=%s)", msg.FromUserName))
		}
	} else {
		common.SysLog(fmt.Sprintf("wechat mp fetch nickname: get access_token failed: %s", err.Error()))
	}

	ok, err := wechat_mp.HandleScanEvent(sceneId, msg.FromUserName, nickname)
	if err != nil {
		common.SysLog("handle wechat mp event failed: " + err.Error())
		c.String(http.StatusOK, "")
		return
	}

	var reply string
	if ok {
		reply = "扫码成功咯～ 跳转网页就能顺利登录"
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

	// 拼接规则：wechat_mp_<maxUserId+1>[_<清洗后的昵称>]
	// 清洗：只保留字母 / 数字 / 中文，剔除 emoji、空格、标点、符号；截断到 16 rune。
	// 昵称为空或清洗后为空（纯 emoji 用户）则不拼后缀，退化为 wechat_mp_<id>。
	cleaned := sanitizeMpNickname(nickname)
	user.Username = "wechat_mp_" + strconv.Itoa(model.GetMaxUserId()+1)
	if cleaned != "" {
		user.Username += "_" + cleaned
	}
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
	// 持久化到 user.InviterId，供后台「邀请人」字段展示与统计使用
	// （Insert 内部会用 inviterId 参数发奖励，但不会回写字段，必须在此显式设置）
	user.InviterId = inviterId

	if err := user.Insert(inviterId); err != nil {
		return nil, err
	}
	user.FinalizeOAuthUserCreation(inviterId)

	// 生成默认令牌（与 controller/oauth.go::HandleOAuth、controller/user.go::Register 保持一致）
	// 受 GENERATE_DEFAULT_TOKEN 环境变量控制
	if constant.GenerateDefaultToken {
		key, err := common.GenerateKey()
		if err != nil {
			common.SysLog("failed to generate default token key: " + err.Error())
		} else {
			token := model.Token{
				UserId:             user.Id,
				Name:               user.Username + "的初始令牌",
				Key:                key,
				CreatedTime:        common.GetTimestamp(),
				AccessedTime:       common.GetTimestamp(),
				ExpiredTime:        -1,
				RemainQuota:        500000,
				UnlimitedQuota:     true,
				ModelLimitsEnabled: false,
			}
			token.Group = "default"
			if err := token.Insert(); err != nil {
				common.SysLog("failed to create default token for user " + user.Username + ": " + err.Error())
			}
		}
	}
	return user, nil
}

// sanitizeMpNickname 清洗微信昵称，使其可作为 username 后缀。
// 规则：
//   - 只保留字母、数字、中文（unicode.IsLetter / IsDigit），保留 CJK / 中文标点
//     （中文标点 IsPunct 为 true 会被剔除）
//   - 剔除 emoji、空格、英文标点、特殊符号
//   - 截断到 16 个 rune（避免 username 过长）
//   - 清洗后为空（例如纯 emoji 昵称）则返回空字符串，调用方应跳过拼接
func sanitizeMpNickname(nickname string) string {
	if nickname == "" {
		return ""
	}
	var b strings.Builder
	count := 0
	for _, r := range nickname {
		if count >= 16 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			count++
		}
		// 其余字符（emoji / 空格 / 标点 / 符号）一律丢弃
	}
	return b.String()
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

	content := fmt.Sprintf(
		"哈喽%s，登录顺利✨\n"+
			"微信扫码安全核验完毕，欢迎落座🍵\n"+
			"往后所有办公琐事，都尽管托付给我就好",
		user.DisplayName,
	)
	// 客户消息有 48 小时窗口限制，扫码事件本身就在窗口内，可直接发送
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
