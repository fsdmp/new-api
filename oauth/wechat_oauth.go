package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func init() {
	Register("wechat-oauth", &WeChatOAuthProvider{})
}

// WeChatOAuthProvider implements OAuth for WeChat Open Platform (website application QR code login)
type WeChatOAuthProvider struct{}

type weChatOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid"`
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
}

type weChatUserInfoResponse struct {
	OpenID     string `json:"openid"`
	Nickname   string `json:"nickname"`
	Sex        int    `json:"sex"`
	Province   string `json:"province"`
	City       string `json:"city"`
	Country    string `json:"country"`
	HeadImgURL string `json:"headimgurl"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

func (p *WeChatOAuthProvider) GetName() string {
	return "WeChat OAuth"
}

func (p *WeChatOAuthProvider) IsEnabled() bool {
	return system_setting.GetWeChatOAuthSettings().Enabled
}

func (p *WeChatOAuthProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}

	logger.LogDebug(ctx, "[OAuth-WeChat] ExchangeToken: code=%s...", code[:min(len(code), 10)])

	settings := system_setting.GetWeChatOAuthSettings()
	url := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/oauth2/access_token?appid=%s&secret=%s&code=%s&grant_type=authorization_code",
		settings.AppId,
		settings.AppSecret,
		code,
	)

	logger.LogDebug(ctx, "[OAuth-WeChat] ExchangeToken: requesting token")

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := http.Client{
		Timeout: 10 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] ExchangeToken error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "WeChat"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-WeChat] ExchangeToken response status: %d", res.StatusCode)

	var tokenResp weChatOAuthTokenResponse
	err = json.NewDecoder(res.Body).Decode(&tokenResp)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] ExchangeToken decode error: %s", err.Error()))
		return nil, err
	}

	if tokenResp.ErrCode != 0 {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] ExchangeToken failed: errcode=%d, errmsg=%s", tokenResp.ErrCode, tokenResp.ErrMsg))
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "WeChat"})
	}

	if tokenResp.AccessToken == "" {
		logger.LogError(ctx, "[OAuth-WeChat] ExchangeToken failed: empty access token")
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "WeChat"})
	}

	logger.LogDebug(ctx, "[OAuth-WeChat] ExchangeToken success: openid=%s", tokenResp.OpenID)

	return &OAuthToken{
		AccessToken: tokenResp.AccessToken,
		ExpiresIn:   tokenResp.ExpiresIn,
		Scope:       tokenResp.Scope,
		Extra: map[string]any{
			"openid":  tokenResp.OpenID,
			"unionid": tokenResp.UnionID,
		},
	}, nil
}

func (p *WeChatOAuthProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	logger.LogDebug(ctx, "[OAuth-WeChat] GetUserInfo: fetching user info")

	openID, _ := token.Extra["openid"].(string)
	if openID == "" {
		logger.LogError(ctx, "[OAuth-WeChat] GetUserInfo failed: missing openid in token")
		return nil, NewOAuthError(i18n.MsgOAuthGetUserErr, nil)
	}

	url := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/userinfo?access_token=%s&openid=%s",
		token.AccessToken,
		openID,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := http.Client{
		Timeout: 10 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "WeChat"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-WeChat] GetUserInfo response status: %d", res.StatusCode)

	if res.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo failed: status=%d", res.StatusCode))
		return nil, NewOAuthError(i18n.MsgOAuthGetUserErr, nil)
	}

	var userInfo weChatUserInfoResponse
	err = json.NewDecoder(res.Body).Decode(&userInfo)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo decode error: %s", err.Error()))
		return nil, err
	}

	if userInfo.ErrCode != 0 {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-WeChat] GetUserInfo failed: errcode=%d, errmsg=%s", userInfo.ErrCode, userInfo.ErrMsg))
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "WeChat"})
	}

	// Prefer unionid as it's unique across all WeChat apps; fallback to openid
	providerUserID := userInfo.UnionID
	if providerUserID == "" {
		providerUserID = userInfo.OpenID
	}

	if providerUserID == "" {
		logger.LogError(ctx, "[OAuth-WeChat] GetUserInfo failed: empty provider user id")
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "WeChat"})
	}

	logger.LogDebug(ctx, "[OAuth-WeChat] GetUserInfo success: openid=%s, nickname=%s", userInfo.OpenID, userInfo.Nickname)

	return &OAuthUser{
		ProviderUserID: providerUserID,
		Username:       userInfo.Nickname,
		DisplayName:    userInfo.Nickname,
	}, nil
}

func (p *WeChatOAuthProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsWeChatIdAlreadyTaken(providerUserID)
}

func (p *WeChatOAuthProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.WeChatId = providerUserID
	return user.FillUserByWeChatId()
}

func (p *WeChatOAuthProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.WeChatId = providerUserID
}

func (p *WeChatOAuthProvider) GetProviderPrefix() string {
	return "wechat_"
}
