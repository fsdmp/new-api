package wechat_mp

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	// 微信 API 基地址
	baseURL = "https://api.weixin.qq.com"
	// access_token Redis 缓存键前缀
	accessTokenKeyPrefix = "wechat_mp:access_token:"
	// access_token 进程内缓存（无 Redis 时降级）
	accessTokenDefaultTTL = 7000 * time.Second
	// HTTP 请求超时
	httpTimeout = 10 * time.Second
)

// 内存缓存（仅在 Redis 不可用时使用）
var (
	memTokenCache sync.Map // map[string]memTokenEntry
)

type memTokenEntry struct {
	token      string
	expireTime time.Time
}

// GetAccessToken 获取公众号 access_token。优先从 Redis 读取，未命中则向微信发起请求。
// appId / appSecret 为公众号凭据。
func GetAccessToken(appId, appSecret string) (string, error) {
	if appId == "" || appSecret == "" {
		return "", errors.New("wechat appid or appsecret is empty")
	}

	redisKey := accessTokenKeyPrefix + appId
	if common.RedisEnabled {
		if token, err := common.RedisGet(redisKey); err == nil && token != "" {
			return token, nil
		}
	}

	// 无缓存，请求微信
	token, ttl, err := requestAccessToken(appId, appSecret)
	if err != nil {
		return "", err
	}

	if common.RedisEnabled {
		_ = common.RedisSet(redisKey, token, time.Duration(ttl)*time.Second)
	} else {
		memTokenCache.Store(appId, memTokenEntry{
			token:      token,
			expireTime: time.Now().Add(time.Duration(ttl) * time.Second),
		})
	}
	return token, nil
}

// requestAccessToken 直接向微信获取 access_token
func requestAccessToken(appId, appSecret string) (string, int, error) {
	u := fmt.Sprintf("%s/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s",
		baseURL, url.QueryEscape(appId), url.QueryEscape(appSecret))

	body, err := httpGetJSON(u)
	if err != nil {
		return "", 0, fmt.Errorf("request access_token: %w", err)
	}

	var res tokenResponse
	if err := common.UnmarshalJsonStr(string(body), &res); err != nil {
		return "", 0, fmt.Errorf("decode access_token response: %w", err)
	}
	if res.ErrCode != 0 || res.AccessToken == "" {
		return "", 0, fmt.Errorf("wechat access_token error: code=%d msg=%s", res.ErrCode, res.ErrMsg)
	}

	ttl := res.ExpiresIn
	if ttl <= 0 || ttl > 7000 {
		ttl = 7000
	}
	return res.AccessToken, ttl, nil
}

// invalidateAccessToken 删除被缓存的 access_token（一般遇到 token 失效时调用）
func InvalidateAccessToken(appId string) {
	redisKey := accessTokenKeyPrefix + appId
	if common.RedisEnabled {
		_ = common.RedisDel(redisKey)
	} else {
		memTokenCache.Delete(appId)
	}
}

// CreateTempQRCode 调用微信接口创建带参数的临时二维码。
// 返回 ticket 和可直接用于 <img src> 的图片 URL。
func CreateTempQRCode(accessToken, sceneId string, expireSecs int) (ticket, qrUrl string, err error) {
	if expireSecs <= 0 || expireSecs > 2592000 {
		expireSecs = 120
	}
	payload := fmt.Sprintf(`{"expire_seconds":%d,"action_name":"QR_STR_SCENE","action_info":{"scene":{"scene_str":%q}}}`,
		expireSecs, sceneId)
	u := fmt.Sprintf("%s/cgi-bin/qrcode/create?access_token=%s", baseURL, url.QueryEscape(accessToken))

	body, err := httpPostJSON(u, payload)
	if err != nil {
		return "", "", fmt.Errorf("create qrcode: %w", err)
	}

	var res qrcodeResponse
	if err := common.UnmarshalJsonStr(string(body), &res); err != nil {
		return "", "", fmt.Errorf("decode qrcode response: %w", err)
	}
	if res.ErrCode != 0 || res.Ticket == "" {
		return "", "", fmt.Errorf("wechat qrcode error: code=%d msg=%s", res.ErrCode, res.ErrMsg)
	}

	ticket = res.Ticket
	qrUrl = fmt.Sprintf("https://mp.weixin.qq.com/cgi-bin/showqrcode?ticket=%s", url.QueryEscape(ticket))
	return ticket, qrUrl, nil
}

// GetUserInfo 获取公众号用户基本信息（昵称等）。失败时返回 error，调用方应自行降级。
func GetUserInfo(accessToken, openid string) (*UserInfo, error) {
	u := fmt.Sprintf("%s/cgi-bin/user/info?access_token=%s&openid=%s&lang=zh_CN",
		baseURL, url.QueryEscape(accessToken), url.QueryEscape(openid))

	body, err := httpGetJSON(u)
	if err != nil {
		return nil, fmt.Errorf("get user info: %w", err)
	}

	var info UserInfo
	if err := common.UnmarshalJsonStr(string(body), &info); err != nil {
		return nil, fmt.Errorf("decode user info: %w", err)
	}
	if info.ErrCode != 0 {
		return nil, fmt.Errorf("wechat user info error: code=%d msg=%s", info.ErrCode, info.ErrMsg)
	}
	return &info, nil
}

// SendCustomTextMessage 通过客服消息接口向用户发送一条文本消息。
// 扫码登录成功后调用，用于给用户在公众号内推送一条登录成功的提示。
func SendCustomTextMessage(accessToken, openid, content string) error {
	if accessToken == "" || openid == "" || content == "" {
		return errors.New("invalid params for custom message")
	}
	payload := fmt.Sprintf(`{"touser":%q,"msgtype":"text","text":{"content":%q}}`, openid, content)
	u := fmt.Sprintf("%s/cgi-bin/message/custom/send?access_token=%s", baseURL, url.QueryEscape(accessToken))

	body, err := httpPostJSON(u, payload)
	if err != nil {
		return fmt.Errorf("send custom message: %w", err)
	}

	// 微信返回 {"errcode":0,"errmsg":"ok"}
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := common.UnmarshalJsonStr(string(body), &res); err != nil {
		return fmt.Errorf("decode custom send response: %w", err)
	}
	if res.ErrCode != 0 {
		return fmt.Errorf("wechat custom send error: code=%d msg=%s", res.ErrCode, res.ErrMsg)
	}
	return nil
}

// httpGetJSON 简单的 GET 请求，返回响应体字节。
func httpGetJSON(rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// httpPostJSON 简单的 POST 请求（application/json），返回响应体字节。
func httpPostJSON(rawURL, payload string) ([]byte, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Post(rawURL, "application/json", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
