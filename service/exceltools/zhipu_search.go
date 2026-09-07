package exceltools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/golang-jwt/jwt/v5"
)

// zhipuProvider implements SearchProvider on Zhipu bigmodel's web-search API
// (web-search-pro family, /api/paas/v4/web_search). The account is shared
// with the GLM channel, so onboarding is zero-friction.
type zhipuProvider struct {
	apiKey string
	client *http.Client
}

const zhipuSearchURL = "https://open.bigmodel.cn/api/paas/v4/web_search"

// zhipuSearchEngine: search_std = standard tier; switch to search_pro for
// higher-quality results at a higher price if verification demands it.
const zhipuSearchEngine = "search_std"

func init() {
	registerSearchProvider("zhipu", func(apiKey string) SearchProvider {
		return &zhipuProvider{
			apiKey: apiKey,
			client: &http.Client{Timeout: searchTimeout},
		}
	})
}

// zhipuToken builds the auth bearer. Zhipu API keys are "<id>.<secret>"; the
// documented auth scheme signs an HS256 JWT with millisecond timestamps.
// Keys without a dot are passed through verbatim (relay-style keys).
func (z *zhipuProvider) zhipuToken() string {
	id, secret, found := strings.Cut(z.apiKey, ".")
	if !found {
		return z.apiKey
	}
	now := time.Now().UnixMilli()
	claims := jwt.MapClaims{
		"api_key":   id,
		"exp":       now + 30*60*1000,
		"timestamp": now,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return z.apiKey
	}
	return signed
}

func (z *zhipuProvider) Name() string { return "zhipu" }

type zhipuSearchResponse struct {
	SearchResult []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		URL     string `json:"url"`
		Content string `json:"content"`
		Media   string `json:"media"`
		Date    string `json:"date"`
		Publish string `json:"publish_date"`
	} `json:"search_result"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (z *zhipuProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	ctx, cancel := searchCtx(ctx)
	defer cancel()

	payload := map[string]interface{}{
		"search_engine": zhipuSearchEngine,
		"search_query":  query,
		"count":         maxResults,
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, zhipuSearchURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+z.zhipuToken())

	resp, err := z.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zhipu network error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("zhipu read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Zhipu signals insufficient balance with 429/402-ish codes inside a
		// 200/4xx; keep the status classification for fallback decisions.
		return nil, &providerHTTPError{Provider: z.Name(), Status: resp.StatusCode, Body: string(respBody)}
	}

	var parsed zhipuSearchResponse
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("zhipu decode: %w", err)
	}
	if parsed.Error.Code != "" {
		// 1113/1114 balance errors are transient in the fallback sense
		status := 500
		if parsed.Error.Code == "1113" || parsed.Error.Code == "1114" || parsed.Error.Code == "1302" {
			status = 402
		}
		return nil, &providerHTTPError{Provider: z.Name(), Status: status, Body: parsed.Error.Code + ": " + parsed.Error.Message}
	}

	results := make([]SearchResult, 0, len(parsed.SearchResult))
	for _, r := range parsed.SearchResult {
		url := r.Link
		if url == "" {
			url = r.URL
		}
		if url == "" || r.Title == "" {
			continue
		}
		date := r.Date
		if date == "" {
			date = r.Publish
		}
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     url,
			Snippet: truncateStr(r.Content, 300),
			Date:    date,
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}
