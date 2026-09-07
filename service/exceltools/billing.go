package exceltools

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// PostToolBilling bills successful virtual-tool executions per call, reusing
// the shared tool-price system (operation_setting.GetToolPriceForModel — the
// same source as relay-side tool billing). Prices default to 0, in which case
// nothing is charged or logged; the mechanism is in place for when pricing is
// turned on later.
func PostToolBilling(c *gin.Context, sess *Session) {
	if len(sess.ToolCounts) == 0 {
		return
	}
	modelName := sess.Model
	if modelName == "" {
		modelName = "unknown"
	}

	totalQuota := 0
	var items []string
	for tool, count := range sess.ToolCounts {
		if count <= 0 {
			continue
		}
		pricePer1K := operation_setting.GetToolPriceForModel(tool, modelName)
		if pricePer1K <= 0 {
			continue
		}
		quota := int(math.Round(pricePer1K * float64(count) / 1000 * common.QuotaPerUnit))
		if quota <= 0 {
			continue
		}
		if err := model.DecreaseUserQuota(sess.UserID, quota, false); err != nil {
			common.SysError(fmt.Sprintf("[excel-tools] tool billing decrease quota failed: user=%d err=%v", sess.UserID, err))
			continue
		}
		totalQuota += quota
		items = append(items, fmt.Sprintf("%s x%d", tool, count))
	}

	if totalQuota <= 0 {
		return
	}
	model.RecordConsumeLog(c, sess.UserID, model.RecordConsumeLogParams{
		ModelName: modelName,
		TokenName: c.GetString("token_name"),
		TokenId:   c.GetInt("token_id"),
		Quota:     totalQuota,
		Content:   fmt.Sprintf("Excel server-tools 按次计费：%s", strings.Join(items, ", ")),
		Other:     map[string]interface{}{"excel_server_tools": true},
	})
}
