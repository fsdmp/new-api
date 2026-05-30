package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type SourceCodeSettings struct {
	SourceCodeURL string `json:"source_code_url"`
}

var defaultSourceCodeSettings = SourceCodeSettings{
	SourceCodeURL: "",
}

func init() {
	config.GlobalConfig.Register("source_code", &defaultSourceCodeSettings)
}

func GetSourceCodeSettings() *SourceCodeSettings {
	return &defaultSourceCodeSettings
}
