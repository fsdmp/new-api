package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type AlipaySettings struct {
	Enabled    bool   `json:"enabled"`
	AppId      string `json:"app_id"`
	PrivateKey string `json:"private_key"`
}

var defaultAlipaySettings = AlipaySettings{}

func init() {
	config.GlobalConfig.Register("alipay", &defaultAlipaySettings)
}

func GetAlipaySettings() *AlipaySettings {
	return &defaultAlipaySettings
}
