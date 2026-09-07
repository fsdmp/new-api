package exceltools

import (
	"github.com/QuantumNous/new-api/common"
)

// emitToolBlocks writes the synthesized server_tool_use + *_tool_result
// content blocks for one iteration's virtual calls, using globally unique
// block indices. This is the single shape-isolation point for client-facing
// server-tool blocks (docs-shape per Anthropic; adjust here if real-device
// verification finds a mismatch).
func emitToolBlocks(lw *lockedWriter, baseIndex int, calls []*CapturedCall, results []*ExecResult) {
	for i, call := range calls {
		idxUse := baseIndex + 2*i
		idxResult := idxUse + 1

		input := call.Input
		if input == nil {
			input = map[string]interface{}{}
		}

		// server_tool_use block: start (empty input) -> input_json_delta -> stop
		writeBlockEvent(lw, "content_block_start", map[string]interface{}{
			"index": idxUse,
			"content_block": map[string]interface{}{
				"type":  "server_tool_use",
				"id":    call.ID,
				"name":  call.Name,
				"input": map[string]interface{}{},
			},
		})
		if encoded, err := common.Marshal(input); err == nil {
			writeBlockEvent(lw, "content_block_delta", map[string]interface{}{
				"index": idxUse,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": string(encoded),
				},
			})
		}
		writeBlockEvent(lw, "content_block_stop", map[string]interface{}{
			"index": idxUse,
		})

		// result block: start carries the full content, then stop
		content := resultContent(call, results[i])
		writeBlockEvent(lw, "content_block_start", map[string]interface{}{
			"index": idxResult,
			"content_block": map[string]interface{}{
				"type":        blockTypeForResult(call.Name),
				"tool_use_id": call.ID,
				"content":     content,
			},
		})
		writeBlockEvent(lw, "content_block_stop", map[string]interface{}{
			"index": idxResult,
		})
	}
}

// emitFinal closes the client stream with aggregated usage and the final
// stop reason.
func emitFinal(lw *lockedWriter, stopReason string, usage UsageAgg) {
	if stopReason == "" {
		stopReason = "end_turn"
	}
	writeBlockEvent(lw, "message_delta", map[string]interface{}{
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"input_tokens":              usage.InputTokens,
			"output_tokens":             usage.OutputTokens,
			"cache_creation_input_tokens": usage.CacheCreationInputTokens,
			"cache_read_input_tokens":     usage.CacheReadInputTokens,
		},
	})
	writeBlockEvent(lw, "message_stop", map[string]interface{}{
		"type": "message_stop",
	})
}

// resultContent builds the client-facing content for one result block:
// full success -> structured block; text-only result (rejection / quota
// denial / empty search) -> its ModelText; executor failure -> error text.
func resultContent(call *CapturedCall, res *ExecResult) interface{} {
	if res != nil && res.Err == nil && res.Block != nil {
		return res.Block
	}
	message := "工具执行失败"
	switch {
	case res == nil:
	case res.Err != nil && res.Err.Message != "":
		message = res.Err.Message
	case res.ModelText != "":
		message = res.ModelText
	}
	return []interface{}{
		map[string]interface{}{"type": "text", "text": message},
	}
}

func blockTypeForResult(toolName string) string {
	switch toolName {
	case "web_search":
		return "web_search_tool_result"
	case "web_fetch":
		return "web_fetch_tool_result"
	case "code_execution":
		return "code_execution_tool_result"
	}
	return "tool_result"
}

func writeBlockEvent(lw *lockedWriter, event string, payload map[string]interface{}) {
	encoded, err := common.Marshal(payload)
	if err != nil {
		return
	}
	lw.emitSSE(event, encoded)
}
