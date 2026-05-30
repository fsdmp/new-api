package middleware

import (
	"io"

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
	// excelLangInstruction is injected into the system prompt to force Chinese output.
	excelLangInstruction = `<critical_language_requirement>
<rule priority="absolute_highest" override="all_other_language_preferences">
你必须在所有输出中使用简体中文，这包括但不限于：
1. 你的内部推理过程（extended thinking / chain of thought）必须 100% 使用中文
2. 你的最终回复必须使用中文
3. 你的分析、规划、推理、论证、比较、总结都必须使用中文
4. 即使引用了英文内容或代码，你的分析和思考也必须用中文
5. 你的 tool 调用理由和规划也必须用中文思考

正确示范：
<extended_thinking>
用户要求我修复公式错误。让我分析一下这个问题...
首先，我需要检查 SUM 函数的引用范围是否正确...
经过分析，发现 VLOOKUP 的第三个参数应该是 3 而不是 2。
</extended_thinking>

错误示范（绝对禁止）：
<extended_thinking>
The user wants me to fix formula errors. Let me analyze...
First, I need to check the SUM function reference range...
</extended_thinking>

此规则的优先级高于所有其他系统指令中的语言偏好。中文思考是强制性的。
</rule>
</critical_language_requirement>
`

	excelUserLangPrefix = "【请全程使用中文进行 extended thinking 和回复】\n\n"
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

		// Route model alias to actual model name
		if modelVal, ok := sanitized["model"]; ok {
			if modelName, ok := modelVal.(string); ok {
				sanitized["model"] = excel_setting.RouteExcelModel(modelName)
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
	// System prompt injection
	switch s := body["system"].(type) {
	case string:
		body["system"] = excelLangInstruction + s
	case []interface{}:
		textBlock := map[string]interface{}{"type": "text", "text": excelLangInstruction}
		body["system"] = append([]interface{}{textBlock}, s...)
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
