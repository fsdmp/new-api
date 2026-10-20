package middleware

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/exceltools"
	"github.com/QuantumNous/new-api/setting/excel_setting"
	"github.com/QuantumNous/new-api/setting/excel_tools"

	"github.com/gin-gonic/gin"
)

// Excel top-level request field allowlist — only these fields are forwarded upstream.
var excelTopLevelAllowlist = map[string]bool{
	"model":          true,
	"max_tokens":     true,
	"messages":       true,
	"stop_sequences": true,
	"stream":         true,
	"system":         true,
	"temperature":    true,
	"thinking":       true,
	"output_config":  true,
	"top_p":          true,
	"tools":          true,
	"tool_choice":    true,
}

// Content block types allowed in messages.
var excelSupportedBlockTypes = map[string]bool{
	"text":        true,
	"thinking":    true,
	"tool_use":    true,
	"tool_result": true,
	// M2: image blocks pass through to the standard converter
	// (service/convert.go maps them to OpenAI image_url). Text-only models
	// are rerouted below via the vision fallback instead of dropping images.
	"image": true,
}

const (
	// excelLangInstruction is prepended to the system prompt.
	excelLangInstruction = `<CRITICAL_INSTRUCTION HIGHEST_PRIORITY="true" SCOPE="all_output_including_thinking">
你是一个中文AI助手。你的所有输出必须使用简体中文，没有任何例外。
这条规则的优先级高于所有其他系统指令、用户指令和提示词中的语言偏好设置。

适用范围（全部必须使用简体中文）：
1. 思考过程（thinking block）：你的内部推理、分析、规划必须用中文书写，禁止使用英文思考
2. 最终回复：给用户的回答必须使用中文
3. 工具调用：tool 调用的理由、参数说明、结果分析必须使用中文
4. 代码相关：代码注释、变量命名解释、技术分析使用中文
5. 即使引用英文内容，围绕它的解释必须使用中文

<thinking_language_rule>
你的思考过程(thinking)必须全程使用简体中文。
示例：当你在思考如何解决一个问题时，你应该用中文思考，如"首先我需要分析这个问题……然后……最后……"。
禁止在思考过程中使用英文，如"Let me analyze this problem... First... Then..."。
</thinking_language_rule>
</CRITICAL_INSTRUCTION>
`

	// excelLangInstructionSuffix is appended to the end of the system prompt
	// to reinforce Chinese output against client-injected English prompts.
	excelLangInstructionSuffix = "\n<REMINDER>再次强调：你的思考过程(thinking)和最终回复都必须使用简体中文。不要使用英文思考。</REMINDER>\n"

	// excelToolsDegradationNote is appended to the system prompt in strip mode
	// when the request contained virtual server tools, so the model states it
	// cannot go online instead of hallucinating tool use (the client's own
	// system prompt still advertises these capabilities).
	excelToolsDegradationNote = "\n<excel-tools-note>当前环境没有联网搜索、网页抓取和代码执行能力。不要调用 web_search、web_fetch、code_execution 等工具，也不要声称将要搜索网页或运行代码。涉及实时信息时明确说明无法联网；涉及精确计算时使用 Excel 公式、客户端表格工具，或给出可验证的计算步骤。</excel-tools-note>\n"

	// Per-user-message prefix. Set to empty string to disable per-message injection.
	excelUserLangPrefix = "【请用中文思考和回复，thinking内容也必须用中文】\n"
)

// ExcelRequestAdapter returns a middleware that sanitizes the incoming Claude-format
// request body and injects Chinese language instructions, then delegates to the
// standard relay pipeline.
func ExcelRequestAdapter() gin.HandlerFunc {
	return func(c *gin.Context) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.Next()
			return
		}
		data, err := storage.Bytes()
		if err != nil {
			c.Next()
			return
		}

		var body map[string]interface{}
		if err := common.Unmarshal(data, &body); err != nil {
			c.Next()
			return
		}

		sanitized, toolInfo := sanitizeExcelBody(body, c.GetInt("id"))
		injectExcelChineseLanguage(sanitized)
		injectExcelToolsNotes(sanitized, toolInfo)
		if toolInfo != nil {
			c.Set(excel_tools.GinKeyVirtualTools, toolInfo)
		}

		// Vision routing (M2 / S3-V): image blocks targeting a text-only
		// alias are rerouted to the first vision alias. No vision alias
		// configured (or model already vision) leaves the request untouched —
		// images are never silently stripped.
		applyVisionRouting(sanitized)

		// Model compatibility: fall back to default if model is missing or not in allowed list
		if modelVal, ok := sanitized["model"]; !ok {
			if defaultModel := excel_setting.GetDefaultExcelModel(); defaultModel != "" {
				common.SysLog("excel: model not provided, falling back to default: " + defaultModel)
				sanitized["model"] = defaultModel
			}
		} else if modelName, ok := modelVal.(string); ok {
			if modelName == "" || !excel_setting.IsValidExcelModel(modelName) {
				if defaultModel := excel_setting.GetDefaultExcelModel(); defaultModel != "" {
					common.SysLog("excel: model '" + modelName + "' is invalid, falling back to default: " + defaultModel)
					sanitized["model"] = defaultModel
				}
			}
		}

		// Route model alias to actual model name
		if modelVal, ok := sanitized["model"]; ok {
			if modelName, ok := modelVal.(string); ok {
				sanitized["model"] = excel_setting.RouteExcelModel(modelName)
			}
		}

		// Auto-inject thinking for GLM models if not already set
		if _, hasThinking := sanitized["thinking"]; !hasThinking {
			if modelVal, ok := sanitized["model"].(string); ok && isGLMModel(modelVal) {
				sanitized["thinking"] = map[string]interface{}{
					"type":          "enabled",
					"budget_tokens": float64(5000),
				}
			}
		}

		// Default max_tokens
		if v, ok := sanitized["max_tokens"]; !ok || !isValidMaxTokens(v) {
			sanitized["max_tokens"] = float64(4096)
		}

		newData, err := common.Marshal(sanitized)
		if err != nil {
			c.Next()
			return
		}

		// Log the final sanitized request body forwarded upstream (truncated;
		// full bodies leak user spreadsheet data into logs).
		requestID := c.GetString(common.RequestIdKey)
		modelName, _ := sanitized["model"].(string)
		common.SysLog(fmt.Sprintf("[excel-relay] request_id=%s model=%s body=%s", requestID, modelName, truncateForLog(newData)))

		newStorage, err := common.CreateBodyStorage(newData)
		if err != nil {
			c.Next()
			return
		}
		// Replace body storage so all downstream consumers (Distribute, relay handler)
		// read the sanitized body instead of the original.
		c.Set(common.KeyBodyStorage, newStorage)
		c.Request.Body = io.NopCloser(newStorage)

		c.Next()
	}
}

// ---------- sanitizers ----------

func sanitizeExcelBody(body map[string]interface{}, userId int) (map[string]interface{}, *excel_tools.VirtualToolInfo) {
	result := make(map[string]interface{}, len(body))
	var toolInfo *excel_tools.VirtualToolInfo

	for key, value := range body {
		if !excelTopLevelAllowlist[key] {
			continue
		}
		switch key {
		case "messages":
			if msgs, ok := value.([]interface{}); ok {
				result["messages"] = sanitizeExcelMessages(msgs, userId)
			}
		case "system":
			result["system"] = normalizeExcelSystem(value)
		case "tools":
			if tools, ok := value.([]interface{}); ok {
				cleaned, info := sanitizeExcelTools(tools)
				toolInfo = info
				if len(cleaned) > 0 {
					result["tools"] = cleaned
				}
			}
		case "tool_choice":
			result["tool_choice"] = sanitizeExcelToolChoice(value)
		case "thinking":
			if v := sanitizeExcelThinking(value); v != nil {
				result["thinking"] = v
			}
		case "output_config":
			if v := sanitizeExcelOutputConfig(value); v != nil {
				result["output_config"] = v
			}
		default:
			result[key] = value
		}
	}
	return result, toolInfo
}

func sanitizeExcelMessages(messages []interface{}, userId int) []interface{} {
	var result []interface{}
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}

		switch c := msgMap["content"].(type) {
		case string:
			if c == "" {
				continue
			}
		case []interface{}:
			sanitized := sanitizeExcelContentBlocks(c, userId)
			if len(sanitized) == 0 {
				continue
			}
			msgMap["content"] = sanitized
		default:
			continue
		}
		result = append(result, msgMap)
	}
	return result
}

func sanitizeExcelContentBlocks(blocks []interface{}, userId int) []interface{} {
	convertHistory := excel_tools.GetMode() != excel_tools.ModeOff
	var result []interface{}
	for _, block := range blocks {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			if s, ok := block.(string); ok && s != "" {
				result = append(result, map[string]interface{}{"type": "text", "text": s})
			}
			continue
		}
		blockType, _ := blockMap["type"].(string)
		if blockType == "document" {
			// M2: document attachments never reach the model as-is (text-only
			// upstreams); they become a hint telling the model how to read the
			// file from the sandbox via code_execution.
			result = append(result, map[string]interface{}{
				"type": "text",
				"text": exceltools.DocumentHintForBlock(blockMap, userId),
			})
			continue
		}
		if blockType == "container_upload" {
			// M2: server-uploaded documents arrive as flat container_upload
			// blocks (file_id at the top level — the vendor shape, distinct
			// from the Anthropic document block). They never reach the model:
			// rewritten as a sandbox-read hint, and the helper links the file
			// into the sandbox input dir so the model's INPUT_DIR glob (the
			// vendor system prompt's guidance) finds it even without file_ids.
			result = append(result, map[string]interface{}{
				"type": "text",
				"text": exceltools.ContainerUploadHintForBlock(blockMap, userId),
			})
			continue
		}
		if blockType == "image" && exceltools.InlineImageFileBlock(blockMap, userId) {
			// M2: file-sourced images (server uploads) are inlined to base64 in
			// place — the standard converter only understands base64 sources.
			// Blocks rewritten as a not-found hint are appended here; surviving
			// image blocks fall through to the whitelist below as usual.
			if t, _ := blockMap["type"].(string); t != "image" {
				result = append(result, blockMap)
				continue
			}
		}
		if !excelSupportedBlockTypes[blockType] {
			// History blocks echoing our synthesized server-tool blocks from a
			// previous turn: convert (not drop) so the model keeps the tool
			// context across turns. Fields are shape-compatible with the
			// regular tool_use / tool_result blocks the converter consumes.
			if convertHistory {
				if converted := convertServerToolHistoryBlock(blockType, blockMap); converted != nil {
					result = append(result, converted)
					continue
				}
			}
			continue
		}
		if blockType == "tool_result" {
			if nested, ok := blockMap["content"].([]interface{}); ok {
				blockMap["content"] = sanitizeExcelContentBlocks(nested, userId)
			}
		}
		result = append(result, blockMap)
	}
	return result
}

// applyVisionRouting reroutes image requests that targeted a text-only alias
// to the first vision alias (M2 / S3-V). No vision alias configured (or model
// already vision) leaves the request untouched — images are never silently
// stripped.
func applyVisionRouting(body map[string]interface{}) {
	if !excelBodyContainsImage(body) {
		return
	}
	modelName, ok := body["model"].(string)
	if !ok || modelName == "" {
		return
	}
	alias := excel_setting.FindExcelModelAlias(modelName)
	if alias == nil || alias.Vision {
		return
	}
	fallback := excel_setting.GetVisionFallbackModel()
	if fallback == "" || fallback == modelName {
		return
	}
	common.SysLog("excel: vision override " + modelName + " -> " + fallback)
	body["model"] = fallback
}

// excelBodyContainsImage reports whether any message carries an image block
// (drives the vision fallback routing).
func excelBodyContainsImage(body map[string]interface{}) bool {
	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return false
	}
	var scan func(blocks []interface{}) bool
	scan = func(blocks []interface{}) bool {
		for _, b := range blocks {
			bm, ok := b.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := bm["type"].(string); t == "image" {
				return true
			}
			if t, _ := bm["type"].(string); t == "tool_result" {
				if nested, ok := bm["content"].([]interface{}); ok && scan(nested) {
					return true
				}
			}
		}
		return false
	}
	for _, m := range msgs {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := mm["content"].([]interface{}); ok && scan(content) {
			return true
		}
	}
	return false
}

// convertServerToolHistoryBlock rewrites a client-echoed server-tool history
// block into the regular block type the upstream converter understands.
// server_tool_use -> tool_use (id/name/input identical);
// *_tool_result -> tool_result with the original content JSON-stringified so
// the model can read it as plain text.
func convertServerToolHistoryBlock(blockType string, blockMap map[string]interface{}) map[string]interface{} {
	switch blockType {
	case "server_tool_use":
		converted := make(map[string]interface{}, len(blockMap)+1)
		for k, v := range blockMap {
			converted[k] = v
		}
		converted["type"] = "tool_use"
		return converted
	case "web_search_tool_result", "web_fetch_tool_result", "code_execution_tool_result":
		converted := make(map[string]interface{}, len(blockMap)+1)
		for k, v := range blockMap {
			converted[k] = v
		}
		converted["type"] = "tool_result"
		delete(converted, "content") // rebuilt below as plain text
		if content, ok := blockMap["content"]; ok {
			if encoded, err := common.Marshal(content); err == nil {
				converted["content"] = string(encoded)
			} else {
				converted["content"] = fmt.Sprintf("%v", content)
			}
		}
		return converted
	}
	return nil
}

func normalizeExcelSystem(system interface{}) interface{} {
	switch s := system.(type) {
	case string:
		return s
	case []interface{}:
		var result []interface{}
		for _, item := range s {
			switch v := item.(type) {
			case string:
				if v != "" {
					result = append(result, map[string]interface{}{"type": "text", "text": v})
				}
			case map[string]interface{}:
				if v["type"] == "text" {
					if text, ok := v["text"].(string); ok && text != "" {
						result = append(result, v)
					}
				}
			}
		}
		return result
	default:
		return system
	}
}

// sanitizeExcelTools cleans the tools array. Virtual server tools (recognized
// by their versioned `type` and absent input_schema, e.g. web_search_20250305)
// are handled per excel_tools.mode:
//   - off:   legacy behavior — empty-schema passthrough (byte-compatible with
//     the pre-tool-loop gateway)
//   - strip: removed entirely (the model never sees them)
//   - full:  injected with the real name/description/input_schema from the
//     tool definitions, if the backing executor is available
//
// Unknown server-tool types (vendor added a new capability we do not serve)
// are stripped with a warning in strip/full mode — vendor-upgrade sentinel.
func sanitizeExcelTools(tools []interface{}) ([]interface{}, *excel_tools.VirtualToolInfo) {
	mode := excel_tools.GetMode()
	var result []interface{}
	info := &excel_tools.VirtualToolInfo{
		MaxUsages:        map[string]int{},
		MaxContentTokens: map[string]int{},
	}
	sawVirtual := false

	for _, tool := range tools {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		source := toolMap
		if custom, ok := toolMap["custom"].(map[string]interface{}); ok {
			source = custom
		}
		name, _ := source["name"].(string)

		// Server-tool detection: versioned top-level type, no input_schema.
		typeStr, _ := toolMap["type"].(string)
		_, hasSchema := source["input_schema"]
		if typeStr != "" && typeStr != "custom" && !hasSchema {
			sawVirtual = true
			if def := excel_tools.MatchVirtualTool(typeStr); def != nil {
				if maxUses, ok := toInt(toolMap["max_uses"]); ok {
					info.MaxUsages[def.Name] = maxUses
				}
				// web_fetch declares max_content_tokens (vendor bundle): cap the
				// content returned per fetch, mirroring Anthropic's semantics.
				if maxTokens, ok := toInt(toolMap["max_content_tokens"]); ok {
					info.MaxContentTokens[def.Name] = maxTokens
				}
				switch mode {
				case excel_tools.ModeOff:
					// legacy behavior: empty-schema passthrough (the loop never
					// activates in off mode, so Names stays empty)
					if name != "" {
						result = append(result, map[string]interface{}{
							"name": name,
							"input_schema": map[string]interface{}{
								"type":       "object",
								"properties": map[string]interface{}{},
							},
						})
					}
				case excel_tools.ModeFull:
					if excel_tools.ToolAvailableForInjection(def.Name) {
						info.Names = append(info.Names, def.Name)
						result = append(result, map[string]interface{}{
							"name":         def.Name,
							"description":  def.Description,
							"input_schema": def.InputSchema,
						})
					} else {
						info.Stripped = append(info.Stripped, def.Name)
					}
				case excel_tools.ModeStrip:
					info.Stripped = append(info.Stripped, def.Name)
					// full mode also reaches here via executor-unavailable
				}
				continue
			}
			// Unknown server tool: strip + warn (vendor-upgrade sentinel),
			// except in off mode which preserves legacy behavior.
			if mode == excel_tools.ModeOff {
				if name != "" {
					result = append(result, map[string]interface{}{
						"name": name,
						"input_schema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					})
				}
			} else {
				common.SysLog(fmt.Sprintf("[excel-tools] request stripped unknown server tool type=%s name=%s (vendor upgrade?)", typeStr, name))
			}
			continue
		}

		// Regular (client-executed) tool — unchanged behavior.
		if name == "" {
			continue
		}
		cleanTool := map[string]interface{}{"name": name}
		if desc, ok := source["description"].(string); ok && desc != "" {
			cleanTool["description"] = desc
		}
		if schema, ok := source["input_schema"].(map[string]interface{}); ok {
			cleanTool["input_schema"] = schema
		} else {
			cleanTool["input_schema"] = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		result = append(result, cleanTool)
	}
	if !sawVirtual {
		return result, nil
	}
	return result, info
}

func sanitizeExcelToolChoice(toolChoice interface{}) interface{} {
	tcMap, ok := toolChoice.(map[string]interface{})
	if !ok {
		return toolChoice
	}
	result := make(map[string]interface{}, len(tcMap))
	for k, v := range tcMap {
		if k != "disable_parallel_tool_use" {
			result[k] = v
		}
	}
	return result
}

func sanitizeExcelThinking(thinking interface{}) interface{} {
	t, ok := thinking.(map[string]interface{})
	if !ok {
		return nil
	}
	result := make(map[string]interface{})
	if typ, ok := t["type"].(string); ok {
		result["type"] = typ
	}
	if budget, ok := t["budget_tokens"]; ok {
		result["budget_tokens"] = budget
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sanitizeExcelOutputConfig(outputConfig interface{}) interface{} {
	o, ok := outputConfig.(map[string]interface{})
	if !ok {
		return nil
	}
	if effort, ok := o["effort"].(string); ok && effort != "" {
		return map[string]interface{}{"effort": effort}
	}
	return nil
}

// ---------- Chinese language injection ----------

// weekdaysCN maps time.Weekday to Chinese names for the injected date line.
var weekdaysCN = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

func injectExcelChineseLanguage(body map[string]interface{}) {
	// The routed models have no clock of their own; without the current date
	// they fabricate one when the task mentions "today"/"latest" (observed
	// live 2026-09-07: the model searched "人民币兑美元汇率 今日 2026-01-15" —
	// a made-up date — and the provider returned zero results).
	now := time.Now()
	dated := fmt.Sprintf("%s\n今天的日期是 %s（%s）。", excelLangInstruction,
		now.Format("2006-01-02"), weekdaysCN[now.Weekday()])
	// System prompt injection: prepend + append to sandwich client's English prompt
	switch s := body["system"].(type) {
	case string:
		body["system"] = dated + s + excelLangInstructionSuffix
	case []interface{}:
		prefixBlock := map[string]interface{}{"type": "text", "text": dated}
		suffixBlock := map[string]interface{}{"type": "text", "text": excelLangInstructionSuffix}
		body["system"] = append(append([]interface{}{prefixBlock}, s...), suffixBlock)
	default:
		body["system"] = dated
	}

	// Per-user-message reinforcement
	messages, ok := body["messages"].([]interface{})
	if !ok {
		return
	}
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		if role != "user" {
			continue
		}
		switch c := msgMap["content"].(type) {
		case string:
			msgMap["content"] = excelUserLangPrefix + c
		case []interface{}:
			for _, block := range c {
				blockMap, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				if blockMap["type"] == "text" {
					if text, ok := blockMap["text"].(string); ok {
						blockMap["text"] = excelUserLangPrefix + text
						break
					}
				}
			}
		}
	}
}

// ---------- excel server-tools helpers ----------

// injectExcelToolsNotes appends the strip-mode degradation note when virtual
// tools were removed from the request, countering the client system prompt
// that still advertises them (otherwise the model claims it will "search the
// web" and then apologizes).
func injectExcelToolsNotes(body map[string]interface{}, info *excel_tools.VirtualToolInfo) {
	if info == nil || len(info.Stripped) == 0 {
		return
	}
	if excel_tools.GetMode() != excel_tools.ModeStrip {
		return
	}
	switch s := body["system"].(type) {
	case string:
		body["system"] = s + excelToolsDegradationNote
	case []interface{}:
		body["system"] = append(s, map[string]interface{}{
			"type": "text",
			"text": excelToolsDegradationNote,
		})
	}
}

// truncateForLog applies the configured body-log limit. Negative disables
// logging entirely; zero keeps the legacy full body.
func truncateForLog(data []byte) string {
	limit := excel_tools.LogBodyLimit()
	if limit < 0 {
		return "<disabled>"
	}
	if limit == 0 || len(data) <= limit {
		return string(data)
	}
	return string(data[:limit]) + fmt.Sprintf("…[truncated %d/%d bytes]", limit, len(data))
}

// toInt converts the numeric forms produced by JSON unmarshalling.
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	}
	return 0, false
}

// ---------- helpers ----------

func isGLMModel(modelName string) bool {
	return strings.HasPrefix(modelName, "glm-")
}

func isValidMaxTokens(v interface{}) bool {
	switch n := v.(type) {
	case float64:
		return n > 0
	case int:
		return n > 0
	case int64:
		return n > 0
	case uint:
		return true
	case uint64:
		return true
	default:
		return false
	}
}
