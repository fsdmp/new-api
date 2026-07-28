package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type WeChatOAuthSettings struct {
	Enabled   bool   `json:"enabled"`
	AppId     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
	// 扫码登录（公众号带参数临时二维码 + SCAN 事件）开关
	ScanLoginEnabled bool `json:"scan_login_enabled"`
	// 公众号后台"服务器配置"中填写的自定义 Token，用于回调签名校验
	Token string `json:"token"`
	// 公众号"服务器配置"中的 EncodingAESKey（43 字符，明文模式可不填，预留扩展）
	EncodingAESKey string `json:"encoding_aes_key"`
}

var defaultWeChatOAuthSettings = WeChatOAuthSettings{}

func init() {
	config.GlobalConfig.Register("wechat_oauth", &defaultWeChatOAuthSettings)
}

func GetWeChatOAuthSettings() *WeChatOAuthSettings {
	return &defaultWeChatOAuthSettings
}
