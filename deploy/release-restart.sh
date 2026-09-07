#!/bin/sh
# 线上重启脚本（make restart-service 会自动上传到 REMOTE_DIR 并执行，也可手工运行）。
#
# 与部署目录里原有的 start.sh/restart.sh/stop.sh 相互独立：
# 本脚本直接重启当前二进制（保留 .prev/.bad 用于回滚），不做 zip 解包清理。
#
# 用法: sh release-restart.sh <二进制名> <工作目录> [启动参数...]
#   例: sh release-restart.sh fsdm-new-api3000 /root/newapi/api --log-dir /opt/fsdm/newapi/logs/3000
set -eu

BIN="${1:?usage: release-restart.sh <bin> <dir> [args...]}"
DIR="${2:?missing workdir}"
shift 2
cd "$DIR"

# 匹配以 ./<bin> 方式启动的进程；排除本脚本($$)与父进程($PPID)，
# 避免调用方命令行里恰好包含 ./<bin> 字样时被误匹配误杀。
# （[.] 字符类写法同时避免 pgrep 匹配到自身命令行）
pids_of_bin() {
	pgrep -f "[.]/$BIN" 2>/dev/null | grep -vw "$$" | grep -vw "$PPID" || true
}

# 优雅停止：先 kill，等待最多 10s，仍存活则 kill -9
PID="$(pids_of_bin)"
if [ -n "$PID" ]; then
	echo "stopping: $PID"
	kill $PID 2>/dev/null || true
	i=0
	while [ $i -lt 10 ] && kill -0 $PID 2>/dev/null; do
		sleep 1
		i=$((i + 1))
	done
	if kill -0 $PID 2>/dev/null; then
		echo "not responding, force kill"
		kill -9 $PID 2>/dev/null || true
	fi
else
	echo "no running process, starting fresh"
fi

# 日志轮转：保留上一轮 output.log，便于排查刚被替换的旧进程
if [ -f output.log ]; then
	mv -f output.log output.log.1
fi

nohup "./$BIN" "$@" > output.log 2>&1 &
sleep 1

NEWPID="$(pids_of_bin)"
if [ -z "$NEWPID" ]; then
	echo "ERROR: process failed to start, last output:"
	tail -n 20 output.log 2>/dev/null || true
	exit 1
fi
echo "started: $NEWPID"
