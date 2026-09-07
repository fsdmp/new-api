package excel_tools

import (
	"os"
	"sync"
	"time"
)

// availabilityCache memoizes filesystem checks so per-request availability
// decisions do not stat the sandbox paths on every message.
var availabilityCache = struct {
	mu      sync.Mutex
	entries map[string]availabilityEntry
}{entries: map[string]availabilityEntry{}}

type availabilityEntry struct {
	ok     bool
	at     time.Time
}

const availabilityCacheTTL = 30 * time.Second

func cachedExists(path string) bool {
	availabilityCache.mu.Lock()
	defer availabilityCache.mu.Unlock()
	if e, ok := availabilityCache.entries[path]; ok && time.Since(e.at) < availabilityCacheTTL {
		return e.ok
	}
	_, err := os.Stat(path)
	ok := err == nil
	availabilityCache.entries[path] = availabilityEntry{ok: ok, at: time.Now()}
	return ok
}

// SandboxDefaultNsjailPath / SandboxDefaultRootfsPath are relative to the
// gateway working directory (bare-metal deployment keeps sandbox artifacts
// next to the binary); both remain configurable.
const (
	SandboxDefaultNsjailPath = "sandbox/nsjail"
	SandboxDefaultRootfsPath = "sandbox/rootfs"
)

// ResolveSandboxPaths returns the configured (or default) sandbox paths.
func ResolveSandboxPaths() (nsjailPath, rootfsPath string) {
	cfg := GetSandboxConfig()
	nsjailPath = cfg.NsjailPath
	if nsjailPath == "" {
		nsjailPath = SandboxDefaultNsjailPath
	}
	rootfsPath = cfg.RootfsPath
	if rootfsPath == "" {
		rootfsPath = SandboxDefaultRootfsPath
	}
	return
}

// SandboxAvailable reports whether the configured runner's artifacts are
// deployed: nsjail needs the binary + rootfs; native needs only the rootfs
// (isolation is pure Go namespaces).
func SandboxAvailable() bool {
	cfg := GetSandboxConfig()
	if !cfg.Enabled {
		return false
	}
	nsjailPath, rootfsPath := ResolveSandboxPaths()
	if cfg.Runner == RunnerNative {
		return cachedExists(rootfsPath)
	}
	return cachedExists(nsjailPath) && cachedExists(rootfsPath)
}

// SearchAvailable reports whether at least one search provider key is configured.
func SearchAvailable() bool {
	cfg := GetSearchConfig()
	for _, key := range cfg.APIKeys {
		if key != "" {
			return true
		}
	}
	return false
}

// ToolAvailableForInjection reports whether the executor behind a virtual tool
// is usable; in full mode an unavailable tool is stripped instead of injected,
// so the model never sees a function that is guaranteed to fail.
func ToolAvailableForInjection(name string) bool {
	switch name {
	case ToolWebSearch:
		return SearchAvailable()
	case ToolWebFetch:
		return true // no external dependencies
	case ToolCodeExecution:
		return SandboxAvailable()
	}
	return false
}
