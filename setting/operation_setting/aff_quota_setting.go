package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// AffQuotaSetting 邀请额度配置
type AffQuotaSetting struct {
	AutoTransferEnabled bool `json:"auto_transfer_enabled"` // 是否自动划转邀请额度到可用余额
}

// 默认配置
var affQuotaSetting = AffQuotaSetting{
	AutoTransferEnabled: false, // 默认关闭
}

func init() {
	config.GlobalConfig.Register("aff_quota_setting", &affQuotaSetting)
}

// GetAffQuotaSetting 获取邀请额度配置
func GetAffQuotaSetting() *AffQuotaSetting {
	return &affQuotaSetting
}

// IsAutoTransferAffQuota 是否自动划转邀请额度
func IsAutoTransferAffQuota() bool {
	return affQuotaSetting.AutoTransferEnabled
}
