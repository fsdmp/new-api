// Package exceltools holds configuration for the Excel gateway server-tools
// loop (web_search / web_fetch / code_execution), mirroring the excel_model_list
// registration pattern: top-level flat fields, complex sub-configs stored as
// JSON strings so the generic config manager can round-trip them.
package excel_tools

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// Mode values for excel_tools.mode.
const (
	ModeOff   = "off"   // legacy behavior: empty-schema passthrough, no loop
	ModeStrip = "strip" // strip virtual tool entries (safe default)
	ModeFull  = "full"  // inject real schemas + run the tool loop
)

type SearchConfig struct {
	Provider string `json:"provider"` // default "zhipu"
	// FallbackProvider is tried once when the primary provider fails with a
	// transient error (network / timeout / 5xx / 429 / insufficient balance).
	// Empty = no fallback.
	FallbackProvider string `json:"fallback_provider"`
	// ProviderByModelPrefix resolves the provider from the routed model name
	// prefix (e.g. {"glm-": "zhipu"}). Falls back to Provider when no prefix
	// matches. Note: DeepSeek has no search API, so deepseek-* prefixes also
	// resolve to zhipu today; the mechanism is in place for future providers.
	ProviderByModelPrefix map[string]string `json:"provider_by_model_prefix"`
	// APIKeys maps provider name -> API key.
	APIKeys    map[string]string `json:"api_keys"`
	MaxResults int               `json:"max_results"` // default 5, hard cap 10
	DailyQuota int               `json:"daily_quota"` // per-user calls/day, default 50
}

type FetchConfig struct {
	TimeoutSec   int   `json:"timeout_sec"`    // default 15
	MaxBodyBytes int64 `json:"max_body_bytes"` // default 5MB
	DailyQuota   int   `json:"daily_quota"`    // default 100
}

type SandboxConfig struct {
	Enabled bool `json:"enabled"`
	// Runner selects the isolation backend: "nsjail" (external binary) or
	// "native" (Go namespace runner, no external binary — bare-metal hosts
	// where shipping a static nsjail is undesirable). Default "nsjail".
	Runner        string `json:"runner"`
	NsjailPath    string `json:"nsjail_path"`    // default: <data>/sandbox/nsjail
	RootfsPath    string `json:"rootfs_path"`    // default: <data>/sandbox/rootfs
	TimeoutSec    int    `json:"timeout_sec"`    // default 30
	MaxConcurrent int    `json:"max_concurrent"` // global concurrent runs, default 4
	DailyQuota    int    `json:"daily_quota"`    // default 50
}

// Sandbox runner backend values.
const (
	RunnerNsjail = "nsjail"
	RunnerNative = "native"
)

type LoopConfig struct {
	MaxIterations   int `json:"max_iterations"`    // default 5 (includes rejection turn)
	PingIntervalSec int `json:"ping_interval_sec"` // default 2
}

// FilesConfig configures the M2 file service (/excel/v1/files, document
// blocks, sandbox /files/input linkage). Bodies live on local disk — the
// production host is bare-metal with no object storage (design doc §2).
type FilesConfig struct {
	Enabled          bool     `json:"enabled"`
	MaxSizeMB        int      `json:"max_size_mb"`          // per file, default 20
	MaxFilesPerUser  int      `json:"max_files_per_user"`   // default 100
	MaxMBPerUser     int      `json:"max_mb_per_user"`      // default 500
	TTLDays          int      `json:"ttl_days"`             // default 30
	Extensions       []string `json:"extensions"`           // upload whitelist
	MaxFileIdsPerRun int      `json:"max_file_ids_per_run"` // code_execution, default 5
}

// ToolsSettings is registered under config key "excel_tools".
type ToolsSettings struct {
	Mode string `json:"mode"` // off | strip | full
	// Sub-configs are stored as JSON strings because the generic config
	// manager only round-trips flat scalar fields.
	Search  string `json:"search"`
	Fetch   string `json:"fetch"`
	Sandbox string `json:"sandbox"`
	Loop    string `json:"loop"`
	Files   string `json:"files"`
	// LogBodyMaxBytes truncates the sanitized-body debug log; 0 keeps the
	// legacy full-body log, negative disables it.
	LogBodyMaxBytes int  `json:"log_body_max_bytes"`
	DebugEndpoints  bool `json:"debug_endpoints"` // POST /excel/api/tools/*
}

const defaultSearchJSON = `{
  "provider": "zhipu",
  "fallback_provider": "",
  "provider_by_model_prefix": {},
  "api_keys": {},
  "max_results": 5,
  "daily_quota": 50
}`

const defaultFetchJSON = `{
  "timeout_sec": 15,
  "max_body_bytes": 5242880,
  "daily_quota": 100
}`

const defaultSandboxJSON = `{
  "enabled": false,
  "runner": "nsjail",
  "nsjail_path": "",
  "rootfs_path": "",
  "timeout_sec": 30,
  "max_concurrent": 4,
  "daily_quota": 50
}`

const defaultLoopJSON = `{
  "max_iterations": 5,
  "ping_interval_sec": 2
}`

const defaultFilesJSON = `{
  "enabled": true,
  "max_size_mb": 20,
  "max_files_per_user": 100,
  "max_mb_per_user": 500,
  "ttl_days": 30,
  "extensions": ["pdf","csv","xlsx","xls","docx","txt","md","json","png","jpg","jpeg","webp","gif"],
  "max_file_ids_per_run": 5
}`

var defaultToolsSettings = ToolsSettings{
	Mode:            ModeStrip,
	Search:          defaultSearchJSON,
	Fetch:           defaultFetchJSON,
	Sandbox:         defaultSandboxJSON,
	Loop:            defaultLoopJSON,
	Files:           defaultFilesJSON,
	LogBodyMaxBytes: 2048,
	DebugEndpoints:  false,
}

func init() {
	config.GlobalConfig.Register("excel_tools", &defaultToolsSettings)
}

// GetMode returns the validated global mode (unknown values -> strip).
func GetMode() string {
	switch defaultToolsSettings.Mode {
	case ModeOff, ModeFull:
		return defaultToolsSettings.Mode
	default:
		return ModeStrip
	}
}

// ---------- cached JSON sub-config parsing ----------

type parsedConfig[T any] struct {
	mu     sync.Mutex
	raw    string
	value  T
	loaded bool
}

func (p *parsedConfig[T]) get(raw string, applyDefaults func(*T)) T {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded && p.raw == raw {
		return p.value
	}
	var v T
	if raw == "" {
		// zero value; defaults applied below
	} else if err := common.Unmarshal([]byte(raw), &v); err != nil {
		common.SysError("excel_tools: failed to parse config JSON: " + err.Error())
		if p.loaded {
			return p.value // keep last good value
		}
		v = *new(T)
	}
	applyDefaults(&v)
	p.raw = raw
	p.value = v
	p.loaded = true
	return v
}

var (
	searchCache  parsedConfig[SearchConfig]
	fetchCache   parsedConfig[FetchConfig]
	sandboxCache parsedConfig[SandboxConfig]
	loopCache    parsedConfig[LoopConfig]
	filesCache   parsedConfig[FilesConfig]
)

// GetSearchConfig returns the search executor config with defaults applied.
func GetSearchConfig() SearchConfig {
	return searchCache.get(defaultToolsSettings.Search, func(c *SearchConfig) {
		if c.Provider == "" {
			c.Provider = "zhipu"
		}
		if c.MaxResults <= 0 {
			c.MaxResults = 5
		}
		if c.MaxResults > 10 {
			c.MaxResults = 10
		}
		if c.DailyQuota <= 0 {
			c.DailyQuota = 50
		}
		if c.APIKeys == nil {
			c.APIKeys = map[string]string{}
		}
		if c.ProviderByModelPrefix == nil {
			c.ProviderByModelPrefix = map[string]string{}
		}
	})
}

// GetFetchConfig returns the web_fetch config with defaults applied.
func GetFetchConfig() FetchConfig {
	return fetchCache.get(defaultToolsSettings.Fetch, func(c *FetchConfig) {
		if c.TimeoutSec <= 0 {
			c.TimeoutSec = 15
		}
		if c.MaxBodyBytes <= 0 {
			c.MaxBodyBytes = 5 * 1024 * 1024
		}
		if c.DailyQuota <= 0 {
			c.DailyQuota = 100
		}
	})
}

// GetSandboxConfig returns the sandbox config with defaults applied.
func GetSandboxConfig() SandboxConfig {
	return sandboxCache.get(defaultToolsSettings.Sandbox, func(c *SandboxConfig) {
		if c.Runner == "" {
			c.Runner = RunnerNsjail
		}
		if c.TimeoutSec <= 0 {
			c.TimeoutSec = 30
		}
		if c.MaxConcurrent <= 0 {
			c.MaxConcurrent = 4
		}
		if c.DailyQuota <= 0 {
			c.DailyQuota = 50
		}
	})
}

// GetLoopConfig returns the loop config with defaults applied.
func GetLoopConfig() LoopConfig {
	return loopCache.get(defaultToolsSettings.Loop, func(c *LoopConfig) {
		if c.MaxIterations <= 0 {
			c.MaxIterations = 5
		}
		if c.PingIntervalSec <= 0 {
			c.PingIntervalSec = 2
		}
	})
}

// GetFilesConfig returns the file-service config with defaults applied.
func GetFilesConfig() FilesConfig {
	return filesCache.get(defaultToolsSettings.Files, func(c *FilesConfig) {
		if c.MaxSizeMB <= 0 {
			c.MaxSizeMB = 20
		}
		if c.MaxFilesPerUser <= 0 {
			c.MaxFilesPerUser = 100
		}
		if c.MaxMBPerUser <= 0 {
			c.MaxMBPerUser = 500
		}
		if c.TTLDays <= 0 {
			c.TTLDays = 30
		}
		if c.MaxFileIdsPerRun <= 0 {
			c.MaxFileIdsPerRun = 5
		}
		if len(c.Extensions) == 0 {
			c.Extensions = []string{"pdf", "csv", "xlsx", "xls", "docx", "txt", "md", "json", "png", "jpg", "jpeg", "webp", "gif"}
		}
	})
}

// FilesEnabled reports whether the M2 file service is on. When off, the
// /excel/v1/files endpoints refuse and document blocks degrade to a notice.
func FilesEnabled() bool { return GetFilesConfig().Enabled }

// LogBodyLimit returns the sanitized-body log truncation limit in bytes.
// 0 = no truncation (legacy), negative = disable body logging.
func LogBodyLimit() int {
	return defaultToolsSettings.LogBodyMaxBytes
}

// DebugEndpointsEnabled reports whether /excel/api/tools/* are exposed.
func DebugEndpointsEnabled() bool {
	return defaultToolsSettings.DebugEndpoints
}

// ApplyForTest overwrites the live settings in place. Unit tests only — the
// config manager keeps writing the same pointer, so field assignment keeps
// both views consistent.
func ApplyForTest(s ToolsSettings) {
	defaultToolsSettings.Mode = s.Mode
	defaultToolsSettings.Search = s.Search
	defaultToolsSettings.Fetch = s.Fetch
	defaultToolsSettings.Sandbox = s.Sandbox
	defaultToolsSettings.Loop = s.Loop
	defaultToolsSettings.Files = s.Files
	defaultToolsSettings.LogBodyMaxBytes = s.LogBodyMaxBytes
	defaultToolsSettings.DebugEndpoints = s.DebugEndpoints
}

// ResetForTest restores factory defaults (pair with ApplyForTest).
func ResetForTest() {
	ApplyForTest(ToolsSettings{
		Mode:            ModeStrip,
		Search:          defaultSearchJSON,
		Fetch:           defaultFetchJSON,
		Sandbox:         defaultSandboxJSON,
		Loop:            defaultLoopJSON,
		Files:           defaultFilesJSON,
		LogBodyMaxBytes: 2048,
		DebugEndpoints:  false,
	})
}
