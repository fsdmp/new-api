package excel_setting

// ExcelModelAlias defines a single alias entry exposed by /excel/v1/models.
type ExcelModelAlias struct {
	ID          string // model id shown to the client
	DisplayName string // human-readable name in the UI
	TargetModel string // actual model name routed to in /messages (empty = passthrough)
}

// GetExcelModelAliases returns the admin-configured model list.
// Only enabled entries are included. Returns empty slice if not configured.
func GetExcelModelAliases() []ExcelModelAlias {
	configured := GetConfiguredModelList()
	if len(configured) == 0 {
		return nil
	}

	result := make([]ExcelModelAlias, 0, len(configured))
	for _, entry := range configured {
		if !entry.Enabled {
			continue
		}
		result = append(result, ExcelModelAlias{
			ID:          entry.ID,
			DisplayName: entry.DisplayName,
			TargetModel: entry.TargetModel,
		})
	}
	return result
}

// RouteExcelModel maps an alias model name to the actual target model name.
// If no mapping exists the name is returned unchanged.
func RouteExcelModel(modelName string) string {
	for _, alias := range GetExcelModelAliases() {
		if alias.ID == modelName && alias.TargetModel != "" {
			return alias.TargetModel
		}
	}
	return modelName
}
