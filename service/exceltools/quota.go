package exceltools

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_tools"

	"github.com/gin-gonic/gin"
)

// QuotaTracker enforces per-user daily call quotas via Redis counters.
// Availability wins over enforcement: when Redis is unavailable the call is
// allowed and logged.
type QuotaTracker struct{}

// NewQuotaTracker creates a tracker (stateless; counters live in Redis).
func NewQuotaTracker() *QuotaTracker { return &QuotaTracker{} }

func dailyQuotaFor(tool string) int {
	switch tool {
	case excel_tools.ToolWebSearch:
		return excel_tools.GetSearchConfig().DailyQuota
	case excel_tools.ToolWebFetch:
		return excel_tools.GetFetchConfig().DailyQuota
	case excel_tools.ToolCodeExecution:
		return excel_tools.GetSandboxConfig().DailyQuota
	}
	return 0
}

// CheckDailyQuota atomically increments today's counter and reports whether
// the caller is over quota (true = deny this call).
func (q *QuotaTracker) CheckDailyQuota(c *gin.Context, userID int, tool string) bool {
	limit := dailyQuotaFor(tool)
	if limit <= 0 {
		return false // unlimited
	}
	if !common.RedisEnabled || common.RDB == nil {
		return false
	}
	key := fmt.Sprintf("excel_tools:q:%s:%d:%s", tool, userID, time.Now().Format("20060102"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	n, err := common.RDB.Incr(ctx, key).Result()
	if err != nil {
		common.SysError("[excel-tools] quota incr failed (allowed): " + err.Error())
		return false
	}
	if n == 1 {
		common.RDB.Expire(ctx, key, 48*time.Hour)
	}
	return int(n) > limit
}
