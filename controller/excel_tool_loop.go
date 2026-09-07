package controller

import (
	"github.com/QuantumNous/new-api/service/exceltools"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// ExcelMessagesRelay is the /excel/v1/messages terminal handler. It decides
// between the zero-cost path (no virtual tools / non-full mode / non-stream)
// and the server-tool loop, which re-enters the unchanged relay pipeline per
// iteration behind an SSE interceptor.
func ExcelMessagesRelay(c *gin.Context) {
	plan := exceltools.BuildLoopPlan(c)
	if !plan.Active {
		Relay(c, types.RelayFormatClaude)
		return
	}
	exceltools.RunToolLoop(c, plan, func(cc *gin.Context) {
		Relay(cc, types.RelayFormatClaude)
	})
}
