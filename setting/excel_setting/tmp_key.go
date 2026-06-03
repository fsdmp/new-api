package excel_setting

import "github.com/QuantumNous/new-api/setting/config"

type TmpKeySettings struct {
	Enabled    bool   `json:"enabled"`
	Account    string `json:"account"`
	ExpireDays int    `json:"expire_days"`
	Quota      int    `json:"quota"`
}

var defaultTmpKeySettings = TmpKeySettings{
	Enabled:    false,
	Account:    "",
	ExpireDays: 7,
	Quota:      500000,
}

func init() {
	config.GlobalConfig.Register("excel_tmp_key", &defaultTmpKeySettings)
}

func GetExcelTmpKeyEnabled() bool {
	return defaultTmpKeySettings.Enabled
}

func GetExcelTmpAccount() string {
	return defaultTmpKeySettings.Account
}

func GetExcelTmpKeyExpireDays() int {
	return defaultTmpKeySettings.ExpireDays
}

func GetExcelTmpKeyQuota() int {
	return defaultTmpKeySettings.Quota
}
