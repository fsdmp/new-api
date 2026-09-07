package exceltools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// SearchResult is the provider-agnostic normalized search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Date    string `json:"date,omitempty"`
}

// SearchProvider abstracts a web search vendor. One implementation per
// provider; adding a vendor = one file + config.
type SearchProvider interface {
	Name() string
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
}

// providerHTTPError marks HTTP-level failures so fallback can classify them
// (transient vs configuration errors).
type providerHTTPError struct {
	Provider string
	Status   int
	Body     string
}

func (e *providerHTTPError) Error() string {
	return fmt.Sprintf("search provider %s returned status %d: %s", e.Provider, e.Status, truncateStr(e.Body, 300))
}

// isTransientSearchError reports whether a provider failure is worth retrying
// on the fallback provider: network errors, timeouts, 5xx, 429, insufficient
// balance. Parameter errors (400) and auth failures (401/403) are not — they
// indicate configuration problems that fallback would only mask.
func isTransientSearchError(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *providerHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status >= 500 || httpErr.Status == 429 || httpErr.Status == 402
	}
	// context deadline / network errors
	return true
}

var searchProviderFactories = map[string]func(apiKey string) SearchProvider{}

func registerSearchProvider(name string, factory func(apiKey string) SearchProvider) {
	searchProviderFactories[name] = factory
}

func newSearchProvider(name string) SearchProvider {
	factory, ok := searchProviderFactories[name]
	if !ok {
		return nil
	}
	key := excel_tools.GetSearchConfig().APIKeys[name]
	if key == "" {
		return nil
	}
	return factory(key)
}

// resolveSearchProvider picks the provider for a request: longest matching
// model prefix in provider_by_model_prefix wins, then the global provider.
func resolveSearchProvider(modelName string) SearchProvider {
	cfg := excel_tools.GetSearchConfig()
	name := cfg.Provider
	for prefix, providerName := range cfg.ProviderByModelPrefix {
		if prefix != "" && strings.HasPrefix(modelName, prefix) {
			name = providerName
			break
		}
	}
	return newSearchProvider(name)
}

// runSearch executes the query on the resolved provider with a single
// fallback attempt (one provider per request — no double billing).
func runSearch(ctx context.Context, modelName, query string, maxResults int) ([]SearchResult, string, error) {
	primary := resolveSearchProvider(modelName)
	if primary == nil {
		return nil, "", fmt.Errorf("search provider not configured (model %s)", modelName)
	}
	results, err := primary.Search(ctx, query, maxResults)
	if err == nil {
		return results, primary.Name(), nil
	}
	if !isTransientSearchError(err) {
		return nil, primary.Name(), err
	}
	cfg := excel_tools.GetSearchConfig()
	if cfg.FallbackProvider == "" || cfg.FallbackProvider == primary.Name() {
		return nil, primary.Name(), err
	}
	fallback := newSearchProvider(cfg.FallbackProvider)
	if fallback == nil {
		return nil, primary.Name(), err
	}
	results, ferr := fallback.Search(ctx, query, maxResults)
	if ferr != nil {
		return nil, primary.Name(), err // report the primary failure
	}
	return results, fallback.Name(), nil
}

func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// searchTimeout bounds one provider call.
const searchTimeout = 10 * time.Second

func searchCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, searchTimeout)
}
