package common

import "strings"

// 不留痕账号（traceless users）：通过 TRACELESS_USERS 环境变量（逗号分隔用户名）配置。
// 命中账号的 API 调用照常计费（余额与令牌额度正常扣减），但不写入消费/错误日志，
// 也不累计任何账号关联统计（users.used_quota/request_count、quota_data 看板数据、
// tokens.used_quota/accessed_time）。
// 名单仅存在于进程内存，不落库；变更需重启进程生效。
var tracelessUsernames = make(map[string]struct{})

// InitTracelessUsers 解析逗号分隔的用户名列表，空白项被忽略。
// 仅在进程启动阶段调用（common.InitEnv），此后 tracelessUsernames 只读。
func InitTracelessUsers(raw string) {
	tracelessUsernames = make(map[string]struct{})
	if raw == "" {
		return
	}
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			tracelessUsernames[name] = struct{}{}
		}
	}
}

// IsTracelessUsername 判断用户名是否在留痕豁免名单中。
func IsTracelessUsername(name string) bool {
	if len(tracelessUsernames) == 0 || name == "" {
		return false
	}
	_, ok := tracelessUsernames[name]
	return ok
}
