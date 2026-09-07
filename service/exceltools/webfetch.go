package exceltools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/exceltools/safefetch"
	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// WebFetchExecutor executes the virtual web_fetch tool.
type WebFetchExecutor struct{}

func init() {
	RegisterExecutor(&WebFetchExecutor{})
}

func (e *WebFetchExecutor) Name() string { return excel_tools.ToolWebFetch }

// fetchModelTextBudget is the absolute cap on page text fed back per fetch.
const fetchModelTextBudget = 80 * 1024

// fetchBudget resolves the effective text budget: the request-declared
// max_content_tokens (vendor web_fetch declaration, ≈4 chars/token) capped by
// the hard default.
func (e *WebFetchExecutor) fetchBudget(sess *Session) int {
	budget := fetchModelTextBudget
	if tokens := sess.MaxContentTokens[excel_tools.ToolWebFetch]; tokens > 0 {
		if byTokens := tokens * 4; byTokens < budget {
			budget = byTokens
		}
	}
	return budget
}

func (e *WebFetchExecutor) Execute(sess *Session, call *CapturedCall) *ExecResult {
	rawURL, ok := call.Param("url")
	if !ok || strings.TrimSpace(rawURL) == "" {
		return invalidInputResult("web_fetch 需要 string 参数 url（要抓取的网页地址，http/https）")
	}

	cfg := excel_tools.GetFetchConfig()
	result, err := safefetch.Fetch(context.Background(), strings.TrimSpace(rawURL), safefetch.Options{
		Timeout:      time.Duration(cfg.TimeoutSec) * time.Second,
		MaxBytes:     int64(cfg.MaxBodyBytes),
		MaxRedirects: 3,
	})
	if err != nil {
		common.SysError(fmt.Sprintf("[excel-tools] web_fetch failed url=%s: %v", truncateStr(rawURL, 200), err))
		return &ExecResult{
			ModelText:   "网页抓取失败，请直接基于已有信息回答，并说明无法访问该链接。",
			Err:         &ToolError{Code: "exec_error", Message: err.Error()},
			StatusShort: "exec_error",
		}
	}

	// Model-facing: title + final url + extracted text within budget.
	budget := e.fetchBudget(sess)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n", result.Title, result.FinalURL)
	text := result.Text
	if len([]rune(text)) > budget {
		text = string([]rune(text)[:budget]) + "\n…（内容过长已截断）"
	}
	b.WriteString(text)
	if result.Truncated {
		b.WriteString("\n（原始页面超过大小上限，仅抓取了前一部分）")
	}
	modelText := b.String()

	// Client-facing block: text content array per Anthropic docs shape.
	return &ExecResult{
		Block: []interface{}{
			map[string]interface{}{
				"type": "web_fetch_result",
				"title": result.Title,
				"url":   result.FinalURL,
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": modelText},
				},
			},
		},
		ModelText:   modelText,
		StatusShort: "ok",
	}
}
