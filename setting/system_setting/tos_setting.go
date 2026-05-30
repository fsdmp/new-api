package system_setting

import "github.com/QuantumNous/new-api/setting/config"

const CurrentTosVersion = "v1"

type TosSettings struct {
	TosVersion string `json:"tos_version"`
}

var defaultTosSettings = TosSettings{
	TosVersion: CurrentTosVersion,
}

func init() {
	config.GlobalConfig.Register("tos", &defaultTosSettings)
}

func GetTosSettings() *TosSettings {
	return &defaultTosSettings
}
