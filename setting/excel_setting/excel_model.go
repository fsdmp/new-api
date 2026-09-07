package excel_setting

// ExcelModelAlias defines a single alias entry exposed by /excel/v1/models.
type ExcelModelAlias struct {
	ID          string // model id shown to the client
	DisplayName string // human-readable name in the UI
	TargetModel string // actual model name routed to in /messages (empty = passthrough)
	Vision      bool   // accepts image blocks (absent in config = true)
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
			Vision:      entry.IsVision(),
		})
	}
	return result
}

// FindExcelModelAlias returns the alias entry for a model id (nil if absent).
func FindExcelModelAlias(modelName string) *ExcelModelAlias {
	aliases := GetExcelModelAliases()
	for i := range aliases {
		if aliases[i].ID == modelName {
			return &aliases[i]
		}
	}
	return nil
}

// GetVisionFallbackModel returns the id of the first enabled vision alias,
// used to reroute image requests that targeted a text-only model. Empty when
// no vision model exists (then the request stays untouched and the upstream
// error surfaces naturally — never silently strip images).
func GetVisionFallbackModel() string {
	for _, alias := range GetExcelModelAliases() {
		if alias.Vision {
			return alias.ID
		}
	}
	return ""
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

// IsValidExcelModel checks whether a model name is in the enabled model alias list.
func IsValidExcelModel(modelName string) bool {
	for _, alias := range GetExcelModelAliases() {
		if alias.ID == modelName {
			return true
		}
	}
	return false
}

// GetDefaultExcelModel returns the ID of the first enabled model alias.
// Returns empty string if no models are configured.
func GetDefaultExcelModel() string {
	aliases := GetExcelModelAliases()
	if len(aliases) == 0 {
		return ""
	}
	return aliases[0].ID
}
