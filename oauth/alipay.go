package oauth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func init() {
	Register("alipay", &AlipayProvider{})
}

// AlipayProvider implements OAuth for Alipay (支付宝)
type AlipayProvider struct{}

type alipayTokenResponse struct {
	AlipaySystemOauthTokenResponse struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		ReExpiresIn  int64  `json:"re_expires_in"`
		UserId       string `json:"user_id"`
		OpenId       string `json:"open_id"`
	} `json:"alipay_system_oauth_token_response"`
	AlipayErrorResponse struct {
		Code    string `json:"code"`
		Msg     string `json:"msg"`
		SubCode string `json:"sub_code"`
		SubMsg  string `json:"sub_msg"`
	} `json:"error_response"`
	Sign string `json:"sign"`
}

type alipayUserInfoResponse struct {
	AlipayUserInfoShareResponse struct {
		UserId   string `json:"user_id"`
		OpenId   string `json:"open_id"`
		NickName string `json:"nick_name"`
		Avatar   string `json:"avatar"`
	} `json:"alipay_user_info_share_response"`
	AlipayErrorResponse struct {
		Code    string `json:"code"`
		Msg     string `json:"msg"`
		SubCode string `json:"sub_code"`
		SubMsg  string `json:"sub_msg"`
	} `json:"error_response"`
	Sign string `json:"sign"`
}

func (p *AlipayProvider) GetName() string {
	return "Alipay"
}

func (p *AlipayProvider) IsEnabled() bool {
	return system_setting.GetAlipaySettings().Enabled
}

func (p *AlipayProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}

	logger.LogDebug(ctx, "[OAuth-Alipay] ExchangeToken: code=%s...", code[:min(len(code), 10)])

	settings := system_setting.GetAlipaySettings()

	params := map[string]string{
		"app_id":     settings.AppId,
		"method":     "alipay.system.oauth.token",
		"charset":    "utf-8",
		"sign_type":  "RSA2",
		"timestamp":  time.Now().Format("2006-01-02 15:04:05"),
		"version":    "1.0",
		"grant_type": "authorization_code",
		"code":       code,
	}

	sign, err := alipaySign(params, settings.PrivateKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] ExchangeToken sign error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Alipay"}, err.Error())
	}
	params["sign"] = sign

	logger.LogDebug(ctx, "[OAuth-Alipay] ExchangeToken: requesting token")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://openapi.alipay.com/gateway.do", strings.NewReader(buildQueryString(params)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] ExchangeToken error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Alipay"}, err.Error())
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] ExchangeToken read body error: %s", err.Error()))
		return nil, err
	}

	logger.LogDebug(ctx, "[OAuth-Alipay] ExchangeToken response status: %d", res.StatusCode)

	var tokenResp alipayTokenResponse
	if err := common.Unmarshal(ensureUTF8(body), &tokenResp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] ExchangeToken decode error: %s", err.Error()))
		return nil, err
	}

	if tokenResp.AlipayErrorResponse.Code != "" {
		errMsg := tokenResp.AlipayErrorResponse.Msg
		if tokenResp.AlipayErrorResponse.SubMsg != "" {
			errMsg = tokenResp.AlipayErrorResponse.SubMsg
		}
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] ExchangeToken failed: code=%s, msg=%s, sub_code=%s, sub_msg=%s",
			tokenResp.AlipayErrorResponse.Code, tokenResp.AlipayErrorResponse.Msg,
			tokenResp.AlipayErrorResponse.SubCode, tokenResp.AlipayErrorResponse.SubMsg))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "Alipay"}, errMsg)
	}

	tokenData := tokenResp.AlipaySystemOauthTokenResponse
	userID := tokenData.OpenId
	if userID == "" {
		userID = tokenData.UserId
	}
	if tokenData.AccessToken == "" || userID == "" {
		logger.LogError(ctx, "[OAuth-Alipay] ExchangeToken failed: empty access token or user identifier")
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "Alipay"})
	}

	logger.LogDebug(ctx, "[OAuth-Alipay] ExchangeToken success: user_id=%s", userID)

	return &OAuthToken{
		AccessToken: tokenData.AccessToken,
		ExpiresIn:   int(tokenData.ExpiresIn),
		Extra: map[string]any{
			"user_id": userID,
		},
	}, nil
}

func (p *AlipayProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	logger.LogDebug(ctx, "[OAuth-Alipay] GetUserInfo: fetching user info")

	userID, _ := token.Extra["user_id"].(string)
	if userID == "" {
		logger.LogError(ctx, "[OAuth-Alipay] GetUserInfo failed: missing user_id in token")
		return nil, NewOAuthError(i18n.MsgOAuthGetUserErr, nil)
	}

	settings := system_setting.GetAlipaySettings()

	params := map[string]string{
		"app_id":    settings.AppId,
		"method":    "alipay.user.info.share",
		"charset":   "utf-8",
		"sign_type": "RSA2",
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"version":   "1.0",
		"auth_token": token.AccessToken,
	}

	sign, err := alipaySign(params, settings.PrivateKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] GetUserInfo sign error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Alipay"}, err.Error())
	}
	params["sign"] = sign

	req, err := http.NewRequestWithContext(ctx, "POST", "https://openapi.alipay.com/gateway.do", strings.NewReader(buildQueryString(params)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] GetUserInfo error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Alipay"}, err.Error())
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] GetUserInfo read body error: %s", err.Error()))
		return nil, err
	}

	var userInfoResp alipayUserInfoResponse
	if err := common.Unmarshal(ensureUTF8(body), &userInfoResp); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] GetUserInfo decode error: %s", err.Error()))
		return nil, err
	}

	if userInfoResp.AlipayErrorResponse.Code != "" {
		errMsg := userInfoResp.AlipayErrorResponse.Msg
		if userInfoResp.AlipayErrorResponse.SubMsg != "" {
			errMsg = userInfoResp.AlipayErrorResponse.SubMsg
		}
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Alipay] GetUserInfo failed: code=%s, msg=%s",
			userInfoResp.AlipayErrorResponse.Code, userInfoResp.AlipayErrorResponse.Msg))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "Alipay"}, errMsg)
	}

	info := userInfoResp.AlipayUserInfoShareResponse
	userID = info.OpenId
	if userID == "" {
		userID = info.UserId
	}
	if userID == "" {
		logger.LogError(ctx, "[OAuth-Alipay] GetUserInfo failed: empty user identifier")
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "Alipay"})
	}

	logger.LogDebug(ctx, "[OAuth-Alipay] GetUserInfo success: user_id=%s, nick_name=%s", userID, info.NickName)

	return &OAuthUser{
		ProviderUserID: userID,
		Username:       info.NickName,
		DisplayName:    info.NickName,
	}, nil
}

func (p *AlipayProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsAlipayIdAlreadyTaken(providerUserID)
}

func (p *AlipayProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.AlipayId = providerUserID
	return user.FillUserByAlipayId()
}

func (p *AlipayProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.AlipayId = providerUserID
}

func (p *AlipayProvider) GetProviderPrefix() string {
	return "alipay_"
}

// alipaySign sorts params alphabetically, concatenates them, and signs with RSA-SHA256
func alipaySign(params map[string]string, privateKeyStr string) (string, error) {
	// Filter out sign only (sign_type must be included in sign content)
	filtered := make(map[string]string)
	for k, v := range params {
		if k == "sign" {
			continue
		}
		if v == "" {
			continue
		}
		filtered[k] = v
	}

	// Sort keys
	keys := make([]string, 0, len(filtered))
	for k := range filtered {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build sign content
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+filtered[k])
	}
	signContent := strings.Join(parts, "&")

	// Parse private key
	privKey, err := parseAlipayPrivateKey(privateKeyStr)
	if err != nil {
		return "", err
	}

	// Sign with SHA256WithRSA
	h := sha256.New()
	h.Write([]byte(signContent))
	hashed := h.Sum(nil)

	signature, err := rsa.SignPKCS1v15(nil, privKey, crypto.SHA256, hashed)
	if err != nil {
		return "", fmt.Errorf("failed to sign: %v", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

// parseAlipayPrivateKey parses a private key from PEM format or raw base64 (PKCS8 or PKCS1).
// It strips all whitespace/newlines from raw base64 content before decoding.
func parseAlipayPrivateKey(privateKeyStr string) (*rsa.PrivateKey, error) {
	// Try PEM format first
	block, _ := pem.Decode([]byte(privateKeyStr))
	if block != nil {
		return parseRSAKey(block.Bytes)
	}

	// Strip all whitespace (spaces, tabs, newlines, carriage returns)
	rawKey := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, privateKeyStr)

	// Try direct base64 decode
	keyBytes, err := base64.StdEncoding.DecodeString(rawKey)
	if err != nil {
		// Try URL-safe base64
		keyBytes, err = base64.URLEncoding.DecodeString(rawKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decode private key: not a valid PEM or base64 key (base64 error: %v)", err)
		}
	}

	return parseRSAKey(keyBytes)
}

func parseRSAKey(data []byte) (*rsa.PrivateKey, error) {
	// Try PKCS8 first (most common for Alipay)
	if key, err := x509.ParsePKCS8PrivateKey(data); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
		return rsaKey, nil
	}
	// Fallback to PKCS1
	if key, err := x509.ParsePKCS1PrivateKey(data); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("failed to parse private key: not a valid PKCS8 or PKCS1 RSA key")
}

// buildQueryString builds a URL-encoded query string from params.
// Values are URL-encoded so that special characters (spaces in timestamp,
// +/=/ in base64 signature) are properly handled when sent as
// application/x-www-form-urlencoded.
func buildQueryString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+url.QueryEscape(params[k]))
	}
	return strings.Join(parts, "&")
}

// ensureUTF8 converts GBK-encoded bytes to UTF-8.
// If the input is already valid UTF-8, it is returned as-is.
func ensureUTF8(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	utf8Data, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return data // fallback to original
	}
	return utf8Data
}
