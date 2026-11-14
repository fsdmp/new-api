package model

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

// 进程内缓存，避免每次判断都回查用户名/令牌归属。
// 缓存按 id 终身有效，因此用户改名或 TRACELESS_USERS 名单变更后需重启进程才能生效。
var (
	tracelessUserCache  sync.Map // userId -> bool
	tracelessTokenCache sync.Map // tokenId -> bool
)

// IsTracelessRequest 依据 gin context 中的用户名（TokenAuth 经 UserBase.WriteContext 写入）
// 判断请求是否来自不留痕账号。未经过 TokenAuth 的路径（如管理员渠道测试）用户名为空，不受影响。
func IsTracelessRequest(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return common.IsTracelessUsername(c.GetString(string(constant.ContextKeyUserName)))
}

// IsTracelessUser 判断用户是否为不留痕账号。
func IsTracelessUser(userId int) bool {
	if userId <= 0 {
		return false
	}
	if v, ok := tracelessUserCache.Load(userId); ok {
		return v.(bool)
	}
	username, err := GetUsernameById(userId, false)
	result := err == nil && common.IsTracelessUsername(username)
	tracelessUserCache.Store(userId, result)
	return result
}

// IsTracelessToken 判断令牌是否属于不留痕账号，tokenId -> userId 的查询每个令牌仅执行一次。
func IsTracelessToken(tokenId int) bool {
	if tokenId <= 0 {
		return false
	}
	if v, ok := tracelessTokenCache.Load(tokenId); ok {
		return v.(bool)
	}
	var userId int
	err := DB.Model(&Token{}).Where("id = ?", tokenId).Select("user_id").Find(&userId).Error
	result := err == nil && IsTracelessUser(userId)
	tracelessTokenCache.Store(tokenId, result)
	return result
}
