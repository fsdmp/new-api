package exceltools

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// WebSearchExecutor executes the virtual web_search tool.
type WebSearchExecutor struct{}

func init() {
	RegisterExecutor(&WebSearchExecutor{})
}

func (e *WebSearchExecutor) Name() string { return excel_tools.ToolWebSearch }

// modelTextBudget caps the numbered list fed back to the model.
const searchModelTextBudget = 4 * 1024

func (e *WebSearchExecutor) Execute(sess *Session, call *CapturedCall) *ExecResult {
	query, ok := call.Param("query")
	if !ok || strings.TrimSpace(query) == "" {
		return invalidInputResult("web_search 需要 string 参数 query（搜索关键词）")
	}

	maxResults := excel_tools.GetSearchConfig().MaxResults
	if n, ok := jsonNumber(call.Input["max_results"]); ok && n > 0 && n <= 10 {
		maxResults = n
	}

	results, usedProvider, err := runSearch(context.Background(), sess.Model, strings.TrimSpace(query), maxResults)
	if err != nil {
		common.SysError(fmt.Sprintf("[excel-tools] search failed provider=%s: %v", usedProvider, err))
		return &ExecResult{
			ModelText:   "联网搜索暂时不可用，请直接基于已有知识回答，并说明未能获取实时信息。",
			Err:         &ToolError{Code: "exec_error", Message: err.Error()},
			StatusShort: "exec_error",
		}
	}
	if len(results) == 0 {
		return &ExecResult{
			ModelText:   "没有搜索到相关结果，请直接回答或调整检索词后重试一次。",
			StatusShort: "ok",
		}
	}

	// Model-facing: compact numbered list within budget. URL-less AI cards
	// (zhipu summarizes some results without a link) still contribute content.
	var b strings.Builder
	fmt.Fprintf(&b, "搜索结果（query: %s）：\n", query)
	for i, r := range results {
		var line string
		if r.URL != "" {
			line = fmt.Sprintf("%d. %s — %s\n%s\n", i+1, r.Title, r.URL, r.Snippet)
		} else {
			line = fmt.Sprintf("%d. %s\n%s\n", i+1, r.Title, r.Snippet)
		}
		if b.Len()+len(line) > searchModelTextBudget {
			b.WriteString("…（其余结果已省略）\n")
			break
		}
		b.WriteString(line)
	}

	// Client-facing block content per Anthropic docs shape. The source panel
	// renders a clickable url, so linkless entries stay model-side only; if
	// nothing has a link, emit no block (falls back to the text result).
	blockContent := make([]interface{}, 0, len(results))
	for _, r := range results {
		if r.URL == "" {
			continue
		}
		blockContent = append(blockContent, map[string]interface{}{
			"type":    "web_search_result",
			"title":   r.Title,
			"url":     r.URL,
			"snippet": r.Snippet,
		})
	}
	if len(blockContent) == 0 {
		blockContent = nil
	}

	return &ExecResult{
		Block:       blockContent,
		ModelText:   b.String(),
		StatusShort: "ok",
	}
}

func invalidInputResult(message string) *ExecResult {
	return &ExecResult{
		ModelText:   "工具调用参数不正确：" + message,
		Err:         &ToolError{Code: "invalid_input", Message: message},
		StatusShort: "invalid_input",
	}
}
