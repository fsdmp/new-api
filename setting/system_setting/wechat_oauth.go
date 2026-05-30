package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type WeChatOAuthSettings struct {
	Enabled   bool   `json:"enabled"`
	AppId     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

var defaultWeChatOAuthSettings = WeChatOAuthSettings{}

func init() {
	config.GlobalConfig.Register("wechat_oauth", &defaultWeChatOAuthSettings)
}

func GetWeChatOAuthSettings() *WeChatOAuthSettings {
	return &defaultWeChatOAuthSettings
}
