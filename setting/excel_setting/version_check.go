package excel_setting

import "github.com/QuantumNous/new-api/setting/config"

type VersionCheckSettings struct {
	MinimumVersions map[string]string `json:"minimum_versions"`
}

var defaultVersionCheckSettings = VersionCheckSettings{
	MinimumVersions: map[string]string{},
}

func init() {
	config.GlobalConfig.Register("excel_version_check", &defaultVersionCheckSettings)
}

// GetMinimumVersion returns the configured minimum version for the given type key
// and true if it exists. Returns "", false if not configured.
func GetMinimumVersion(versionType string) (string, bool) {
	v, ok := defaultVersionCheckSettings.MinimumVersions[versionType]
	return v, ok
}
