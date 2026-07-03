package middleware

import (
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_setting"

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

		sanitized := sanitizeExcelBody(body)
		injectExcelChineseLanguage(sanitized)

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

		// Log the final sanitized request body forwarded upstream.
		requestID := c.GetString(common.RequestIdKey)
		modelName, _ := sanitized["model"].(string)
		common.SysLog(fmt.Sprintf("[excel-relay] request_id=%s model=%s body=%s", requestID, modelName, string(newData)))

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

func sanitizeExcelBody(body map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(body))

	for key, value := range body {
		if !excelTopLevelAllowlist[key] {
			continue
		}
		switch key {
		case "messages":
			if msgs, ok := value.([]interface{}); ok {
				result["messages"] = sanitizeExcelMessages(msgs)
			}
		case "system":
			result["system"] = normalizeExcelSystem(value)
		case "tools":
			if tools, ok := value.([]interface{}); ok {
				if cleaned := sanitizeExcelTools(tools); len(cleaned) > 0 {
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
	return result
}

func sanitizeExcelMessages(messages []interface{}) []interface{} {
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
			sanitized := sanitizeExcelContentBlocks(c)
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

func sanitizeExcelContentBlocks(blocks []interface{}) []interface{} {
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
		if !excelSupportedBlockTypes[blockType] {
			continue
		}
		if blockType == "tool_result" {
			if nested, ok := blockMap["content"].([]interface{}); ok {
				blockMap["content"] = sanitizeExcelContentBlocks(nested)
			}
		}
		result = append(result, blockMap)
	}
	return result
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

func sanitizeExcelTools(tools []interface{}) []interface{} {
	var result []interface{}
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
	return result
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

func injectExcelChineseLanguage(body map[string]interface{}) {
	// System prompt injection: prepend + append to sandwich client's English prompt
	switch s := body["system"].(type) {
	case string:
		body["system"] = excelLangInstruction + s + excelLangInstructionSuffix
	case []interface{}:
		prefixBlock := map[string]interface{}{"type": "text", "text": excelLangInstruction}
		suffixBlock := map[string]interface{}{"type": "text", "text": excelLangInstructionSuffix}
		body["system"] = append(append([]interface{}{prefixBlock}, s...), suffixBlock)
	default:
		body["system"] = excelLangInstruction
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
