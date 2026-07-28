package wechat_mp

import (
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// MpSessionTTL 公众号扫码登录会话状态在 Redis 中的存活时间
const MpSessionTTL = 180 * time.Second

// mpKeyPrefix 公众号扫码登录会话 Redis 键前缀
const mpKeyPrefix = "wechat_mp:session:"

// mpStateKey 拼接某个 sceneId 的 Redis 键
func mpStateKey(sceneId string) string {
	return mpKeyPrefix + sceneId
}

// VerifySignature 微信公众号服务器配置签名校验（标准 SHA1 算法）
// 算法：sha1(sort([token, timestamp, nonce]))
func VerifySignature(token, timestamp, nonce, signature string) bool {
	if token == "" || timestamp == "" || nonce == "" || signature == "" {
		return false
	}
	arr := []string{token, timestamp, nonce}
	sort.Strings(arr)
	h := sha1.New()
	if _, err := io.WriteString(h, strings.Join(arr, "")); err != nil {
		return false
	}
	computed := fmt.Sprintf("%x", h.Sum(nil))
	return computed == signature
}

// CreateMpSession 在 Redis 中创建一个 pending 状态的公众号扫码登录会话
func CreateMpSession(sceneId, affCode string) error {
	state := MpLoginState{
		SceneId:   sceneId,
		Status:    StatusPending,
		CreatedAt: time.Now().Unix(),
		AffCode:   affCode,
	}
	data, err := common.Marshal(state)
	if err != nil {
		return err
	}
	return common.RedisSet(mpStateKey(sceneId), string(data), MpSessionTTL)
}

// GetMpSession 读取公众号扫码登录会话状态
func GetMpSession(sceneId string) (*MpLoginState, error) {
	raw, err := common.RedisGet(mpStateKey(sceneId))
	if err != nil {
		return nil, err
	}
	var state MpLoginState
	if err := common.UnmarshalJsonStr(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// MarkScanned 在 Redis 中把会话标记为 scanned（已扫码）并写入 openid/nickname。
// 仅当当前 status==pending 时才会更新，其他状态返回 false 表示不可变更。
func MarkScanned(sceneId, openID, nickname string) (bool, error) {
	state, err := GetMpSession(sceneId)
	if err != nil {
		return false, err
	}
	if state.Status != StatusPending {
		// 已被处理（例如重复扫码），不变更
		return false, nil
	}
	state.Status = StatusScanned
	state.OpenID = openID
	state.Nickname = nickname
	data, err := common.Marshal(state)
	if err != nil {
		return false, err
	}
	if err := common.RedisSet(mpStateKey(sceneId), string(data), MpSessionTTL); err != nil {
		return false, err
	}
	return true, nil
}

// MarkLoggedIn 标记会话为 logged_in 并立即删除 Redis 键以防重放。
// 返回的 MpLoginState 可供控制器取出 affCode/openid 等信息。
func MarkLoggedIn(sceneId string, userId int) (*MpLoginState, error) {
	state, err := GetMpSession(sceneId)
	if err != nil {
		return nil, err
	}
	if state.Status != StatusScanned {
		return nil, fmt.Errorf("invalid status: %s", state.Status)
	}
	state.Status = StatusLoggedIn
	state.UserID = userId
	// 不需要持久化 logged_in，直接删除以防重放
	_ = common.RedisDel(mpStateKey(sceneId))
	return state, nil
}

// DeleteMpSession 主动删除公众号扫码登录会话（例如生成新二维码时清理旧的）
func DeleteMpSession(sceneId string) {
	_ = common.RedisDel(mpStateKey(sceneId))
}

// ParseMessage 从请求 Body 中解析微信 XML 消息
func ParseMessage(reader io.Reader) (*WXMessage, error) {
	var msg WXMessage
	if err := xml.NewDecoder(reader).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ExtractSceneId 根据事件类型（subscribe/SCAN）解析出 sceneId
func ExtractSceneId(msg *WXMessage) string {
	if msg == nil {
		return ""
	}
	switch msg.Event {
	case "subscribe":
		// 未关注用户扫码：EventKey 形如 "qrscene_xxx"
		return strings.TrimPrefix(msg.EventKey, "qrscene_")
	case "SCAN":
		// 已关注用户扫码：EventKey 直接是 sceneId
		return msg.EventKey
	default:
		return ""
	}
}

// ReplyTextXML 构造回复给微信用户的文本消息 XML
func ReplyTextXML(toUser, fromUser, content string) string {
	// 注意：回复消息的 ToUserName 是消息发送方（用户 openid），FromUserName 是公众号
	return fmt.Sprintf(`<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`, toUser, fromUser, time.Now().Unix(), content)
}

// HandleScanEvent 处理扫码/关注事件：将 session 标记为 scanned 并写入 openid/nickname。
// 返回值 ok=true 表示成功变更；ok=false 表示 sceneId 未找到或会话非 pending（重复扫码）。
// 函数名保留 Scan 因为它直接对应微信协议中的 SCAN 事件。
func HandleScanEvent(sceneId, openID, nickname string) (bool, error) {
	if sceneId == "" || openID == "" {
		return false, nil
	}
	return MarkScanned(sceneId, openID, nickname)
}
