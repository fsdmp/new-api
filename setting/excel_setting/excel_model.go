package excel_setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ExcelModelAlias defines a single alias entry exposed by /excel/v1/models.
type ExcelModelAlias struct {
	ID          string // model id shown to the client
	DisplayName string // human-readable name in the UI
	TargetModel string // actual model name routed to in /messages (empty = passthrough)
}

var (
	excelModelAliases     []ExcelModelAlias
	excelModelAliasesOnce sync.Once

	excelTmpAccount       string
	excelTmpKeyExpireDays int
	excelTmpKeyQuota      int
	excelTmpKeyEnabled    bool
	excelTmpKeyOnce       sync.Once
)

// loadExcelModelAliases reads alias configuration from environment variables.
//
// Environment variables (all optional, with sensible defaults):
//
//	EXCEL_MODEL_PRIMARY   – primary model id (default "deepseek-v4-pro")
//	EXCEL_MODEL_FAST      – fast model id (default "deepseek-v4-flash")
//	EXCEL_ALIAS_SONNET    – sonnet alias id (default "claude-sonnet-4-6")
//	EXCEL_ALIAS_OPUS      – opus alias id (default "claude-opus-4-1")
//	EXCEL_ALIAS_HAIKU     – haiku alias id (default "claude-3-5-haiku-latest")
//	EXCEL_MODEL_MAPPING   – custom JSON mapping, e.g. {"display-name":"actual-model"}
//	                          Each key becomes an alias with TargetModel = value.
func loadExcelModelAliases() {
	primary := common.GetEnvOrDefaultString("EXCEL_MODEL_PRIMARY", "deepseek-v4-pro")
	fast := common.GetEnvOrDefaultString("EXCEL_MODEL_FAST", "deepseek-v4-flash")
	aliasSonnet := common.GetEnvOrDefaultString("EXCEL_ALIAS_SONNET", "claude-sonnet-4-6")
	aliasOpus := common.GetEnvOrDefaultString("EXCEL_ALIAS_OPUS", "claude-opus-4-1")
	aliasHaiku := common.GetEnvOrDefaultString("EXCEL_ALIAS_HAIKU", "claude-3-5-haiku-latest")

	excelModelAliases = []ExcelModelAlias{
		{ID: primary, DisplayName: "DeepSeekV4", TargetModel: ""},
		{ID: aliasSonnet, DisplayName: "Claude Sonnet", TargetModel: primary},
		{ID: aliasOpus, DisplayName: "Claude Opus", TargetModel: primary},
		{ID: aliasHaiku, DisplayName: "Claude Haiku", TargetModel: fast},
	}
}

// GetExcelModelAliases returns the configured alias list (lazy-loaded once).
func GetExcelModelAliases() []ExcelModelAlias {
	excelModelAliasesOnce.Do(loadExcelModelAliases)
	return excelModelAliases
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

// loadExcelTmpKeyConfig reads temporary key configuration from environment variables.
//
// Environment variables:
//
//	EXCEL_TMP_KEY_ENABLED    – enable/disable the tmp-key endpoint (default false)
//	EXCEL_TMP_ACCOUNT        – username of the public account that owns temporary tokens (required)
//	EXCEL_TMP_KEY_EXPIRE_DAYS – token expiration in days (default 7)
//	EXCEL_TMP_KEY_QUOTA       – token quota in internal units (default 500000)
func loadExcelTmpKeyConfig() {
	excelTmpKeyEnabled = common.GetEnvOrDefaultBool("EXCEL_TMP_KEY_ENABLED", false)
	excelTmpAccount = common.GetEnvOrDefaultString("EXCEL_TMP_ACCOUNT", "")
	excelTmpKeyExpireDays = common.GetEnvOrDefault("EXCEL_TMP_KEY_EXPIRE_DAYS", 7)
	excelTmpKeyQuota = common.GetEnvOrDefault("EXCEL_TMP_KEY_QUOTA", 500000)
}

func ensureExcelTmpKeyConfig() {
	excelTmpKeyOnce.Do(loadExcelTmpKeyConfig)
}

func GetExcelTmpKeyEnabled() bool {
	ensureExcelTmpKeyConfig()
	return excelTmpKeyEnabled
}

func GetExcelTmpAccount() string {
	ensureExcelTmpKeyConfig()
	return excelTmpAccount
}

func GetExcelTmpKeyExpireDays() int {
	ensureExcelTmpKeyConfig()
	return excelTmpKeyExpireDays
}

func GetExcelTmpKeyQuota() int {
	ensureExcelTmpKeyConfig()
	return excelTmpKeyQuota
}
