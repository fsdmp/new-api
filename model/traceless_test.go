package model

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 复用 task_cas_test.go 的 TestMain（内存 SQLite，BatchUpdateEnabled=false，LogConsumeEnabled=true）。

func resetTracelessCaches() {
	tracelessUserCache.Range(func(key, _ any) bool {
		tracelessUserCache.Delete(key)
		return true
	})
	tracelessTokenCache.Range(func(key, _ any) bool {
		tracelessTokenCache.Delete(key)
		return true
	})
}

// newTracelessFixture 建一个用户 + 令牌，令牌初始 remain_quota=500000、used_quota=0、accessed_time=0。
// 子测试结束时清理行数据与进程内 traceless 缓存（SQLite rowid 会复用，避免陈旧缓存误判下一个子测试）。
func newTracelessFixture(t *testing.T, username string) (*User, *Token) {
	t.Helper()
	u := &User{Username: username, Status: common.UserStatusEnabled, Quota: 1000000}
	require.NoError(t, u.Insert(0))
	tok := &Token{UserId: u.Id, Key: "tk-" + username, Name: username, Status: common.TokenStatusEnabled, RemainQuota: 500000}
	require.NoError(t, tok.Insert())
	t.Cleanup(func() {
		DB.Exec("DELETE FROM logs WHERE user_id = ?", u.Id)
		DB.Exec("DELETE FROM tokens WHERE id = ?", tok.Id)
		DB.Exec("DELETE FROM users WHERE id = ?", u.Id)
		tracelessUserCache.Delete(u.Id)
		tracelessTokenCache.Delete(tok.Id)
	})
	return u, tok
}

func newTracelessTestContext(username string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if username != "" {
		c.Set("username", username)
	}
	return c
}

func reloadToken(t *testing.T, id int) Token {
	t.Helper()
	var tok Token
	require.NoError(t, DB.First(&tok, id).Error)
	return tok
}

func countLogs(t *testing.T, userId int) int64 {
	t.Helper()
	var n int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("user_id = ?", userId).Count(&n).Error)
	return n
}

func resetQuotaDataCache(t *testing.T) {
	t.Helper()
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
}

func quotaDataCacheLen(t *testing.T) int {
	t.Helper()
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	return len(CacheQuotaData)
}

func TestTracelessGuards(t *testing.T) {
	truncateTables(t)
	common.InitTracelessUsers("ghost")
	resetTracelessCaches()
	t.Cleanup(func() {
		common.InitTracelessUsers("")
		resetTracelessCaches()
	})

	t.Run("IsTracelessUser", func(t *testing.T) {
		ghost, _ := newTracelessFixture(t, "ghost")
		normal, _ := newTracelessFixture(t, "tl_normal1")
		assert.True(t, IsTracelessUser(ghost.Id))
		assert.False(t, IsTracelessUser(normal.Id))
	})

	t.Run("UpdateUserUsedQuotaAndRequestCount", func(t *testing.T) {
		ghost, _ := newTracelessFixture(t, "ghost")
		normal, _ := newTracelessFixture(t, "tl_normal2")

		UpdateUserUsedQuotaAndRequestCount(ghost.Id, 3000)
		var g User
		require.NoError(t, DB.First(&g, ghost.Id).Error)
		assert.Zero(t, g.UsedQuota, "ghost used_quota 不应累计")
		assert.Zero(t, g.RequestCount, "ghost request_count 不应累计")

		UpdateUserUsedQuotaAndRequestCount(normal.Id, 3000)
		var n User
		require.NoError(t, DB.First(&n, normal.Id).Error)
		assert.EqualValues(t, 3000, n.UsedQuota, "普通用户 used_quota 照常累计")
		assert.EqualValues(t, 1, n.RequestCount, "普通用户 request_count 照常累计")
	})

	t.Run("DecreaseTokenQuota", func(t *testing.T) {
		_, ghostToken := newTracelessFixture(t, "ghost")
		_, normalToken := newTracelessFixture(t, "tl_normal3")

		require.NoError(t, DecreaseTokenQuota(ghostToken.Id, ghostToken.Key, 2000))
		g := reloadToken(t, ghostToken.Id)
		assert.EqualValues(t, 500000-2000, g.RemainQuota, "ghost remain_quota 照常扣减")
		assert.Zero(t, g.UsedQuota, "ghost token used_quota 不应累计")
		assert.Zero(t, g.AccessedTime, "ghost accessed_time 不应刷新")

		require.NoError(t, DecreaseTokenQuota(normalToken.Id, normalToken.Key, 2000))
		n := reloadToken(t, normalToken.Id)
		assert.EqualValues(t, 500000-2000, n.RemainQuota)
		assert.EqualValues(t, 2000, n.UsedQuota, "普通用户 token used_quota 照常累计")
		assert.Positive(t, n.AccessedTime, "普通用户 accessed_time 照常刷新")
	})

	t.Run("IncreaseTokenQuota", func(t *testing.T) {
		_, ghostToken := newTracelessFixture(t, "ghost")

		require.NoError(t, DecreaseTokenQuota(ghostToken.Id, ghostToken.Key, 2000))
		require.NoError(t, IncreaseTokenQuota(ghostToken.Id, ghostToken.Key, 500))
		g := reloadToken(t, ghostToken.Id)
		assert.EqualValues(t, 500000-2000+500, g.RemainQuota, "ghost remain_quota 差额照常退回")
		assert.Zero(t, g.UsedQuota, "ghost token used_quota 不应累计")
		assert.Zero(t, g.AccessedTime, "ghost accessed_time 不应刷新")
	})

	t.Run("RecordConsumeLog", func(t *testing.T) {
		ghost, _ := newTracelessFixture(t, "ghost")
		normal, _ := newTracelessFixture(t, "tl_normal4")

		common.DataExportEnabled = true
		t.Cleanup(func() { common.DataExportEnabled = false })
		resetQuotaDataCache(t)

		RecordConsumeLog(newTracelessTestContext("ghost"), ghost.Id, RecordConsumeLogParams{Quota: 300, ModelName: "gpt-test"})
		assert.Zero(t, countLogs(t, ghost.Id), "ghost 不应写消费日志")
		time.Sleep(100 * time.Millisecond) // LogQuotaData 为异步写入，等待后仍应无任何条目
		assert.Zero(t, quotaDataCacheLen(t), "ghost 不应进看板数据缓存")

		RecordConsumeLog(newTracelessTestContext("tl_normal4"), normal.Id, RecordConsumeLogParams{Quota: 300, ModelName: "gpt-test"})
		assert.EqualValues(t, 1, countLogs(t, normal.Id), "普通用户消费日志照常写入")
		time.Sleep(100 * time.Millisecond)
		assert.EqualValues(t, 1, quotaDataCacheLen(t), "普通用户应进看板数据缓存")
	})

	t.Run("RecordErrorLog", func(t *testing.T) {
		ghost, _ := newTracelessFixture(t, "ghost")
		normal, _ := newTracelessFixture(t, "tl_normal5")

		RecordErrorLog(newTracelessTestContext("ghost"), ghost.Id, 1, "gpt-test", "tk", "boom", 0, 1, false, "default", nil)
		assert.Zero(t, countLogs(t, ghost.Id), "ghost 不应写错误日志")

		RecordErrorLog(newTracelessTestContext("tl_normal5"), normal.Id, 1, "gpt-test", "tk", "boom", 0, 1, false, "default", nil)
		assert.EqualValues(t, 1, countLogs(t, normal.Id), "普通用户错误日志照常写入")
	})

	t.Run("RecordTaskBillingLog", func(t *testing.T) {
		ghost, _ := newTracelessFixture(t, "ghost")
		normal, _ := newTracelessFixture(t, "tl_normal6")

		RecordTaskBillingLog(RecordTaskBillingLogParams{UserId: ghost.Id, LogType: LogTypeConsume, Quota: 300})
		assert.Zero(t, countLogs(t, ghost.Id), "ghost 不应写任务计费日志")

		RecordTaskBillingLog(RecordTaskBillingLogParams{UserId: normal.Id, LogType: LogTypeConsume, Quota: 300})
		assert.EqualValues(t, 1, countLogs(t, normal.Id), "普通用户任务计费日志照常写入")
	})

	t.Run("BatchUpdateTokenQuotaNoStats", func(t *testing.T) {
		common.BatchUpdateEnabled = true
		t.Cleanup(func() { common.BatchUpdateEnabled = false })

		_, ghostToken := newTracelessFixture(t, "ghost")
		_, normalToken := newTracelessFixture(t, "tl_normal7")

		require.NoError(t, DecreaseTokenQuota(ghostToken.Id, ghostToken.Key, 2000))
		require.NoError(t, DecreaseTokenQuota(normalToken.Id, normalToken.Key, 2000))
		batchUpdate()

		g := reloadToken(t, ghostToken.Id)
		assert.EqualValues(t, 500000-2000, g.RemainQuota, "batch 模式 ghost remain_quota 照常扣减")
		assert.Zero(t, g.UsedQuota, "batch 模式 ghost used_quota 不应累计")
		assert.Zero(t, g.AccessedTime, "batch 模式 ghost accessed_time 不应刷新")

		n := reloadToken(t, normalToken.Id)
		assert.EqualValues(t, 500000-2000, n.RemainQuota)
		assert.EqualValues(t, 2000, n.UsedQuota, "batch 模式普通用户照常累计")
		assert.Positive(t, n.AccessedTime)
	})
}
