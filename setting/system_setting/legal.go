package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type LegalSettings struct {
	UserAgreement  string `json:"user_agreement"`
	PrivacyPolicy  string `json:"privacy_policy"`
	TermsOfService string `json:"terms_of_service"`
	SLA            string `json:"sla"`
	DPA            string `json:"dpa"`
}

var defaultLegalSettings = LegalSettings{
	UserAgreement:  "",
	PrivacyPolicy:  "",
	TermsOfService: "",
	SLA:            "",
	DPA:            "",
}

func init() {
	config.GlobalConfig.Register("legal", &defaultLegalSettings)
}

func GetLegalSettings() *LegalSettings {
	return &defaultLegalSettings
}
