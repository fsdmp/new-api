package exceltools

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

// ExecuteDebugCall runs ONE virtual tool call outside the relay loop, for the
// debug endpoints (POST /excel/api/tools/{name}). It goes through the same
// executeOne path as production calls: quota check, logging, per-tool billing
// counters — a debug call behaves exactly like a real one.
func ExecuteDebugCall(c *gin.Context, tool, model string, input map[string]interface{}) *ExecResult {
	sess := &Session{
		RequestID:  c.GetString(common.RequestIdKey),
		UserID:     c.GetInt("id"),
		Model:      model,
		ToolCounts: map[string]int{},
		Quota:      NewQuotaTracker(),
	}
	call := &CapturedCall{ID: "debug-" + tool, Name: tool, Input: input}
	res := executeOne(c, sess, call)
	PostToolBilling(c, sess) // billed identically to loop calls (default 0)
	return res
}
