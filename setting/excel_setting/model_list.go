package excel_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// ExcelModelEntry defines a single model entry in the configurable model list.
type ExcelModelEntry struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	TargetModel string `json:"target_model"`
	Enabled     bool   `json:"enabled"`
}

// ModelListSettings holds the configurable model list for the Excel API.
// Models is stored as a JSON string of []ExcelModelEntry.
type ModelListSettings struct {
	Models string `json:"models"`
}

var defaultModelListSettings = ModelListSettings{
	Models: "[]",
}

func init() {
	config.GlobalConfig.Register("excel_model_list", &defaultModelListSettings)
}

// GetConfiguredModelList parses the configured model list JSON.
// Returns nil if the list is empty or parsing fails.
func GetConfiguredModelList() []ExcelModelEntry {
	raw := defaultModelListSettings.Models
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}

	var entries []ExcelModelEntry
	if err := common.Unmarshal([]byte(raw), &entries); err != nil {
		common.SysError("failed to parse excel_model_list.models: " + err.Error())
		return nil
	}

	if len(entries) == 0 {
		return nil
	}

	return entries
}
