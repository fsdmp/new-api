#!/bin/sh
# new-api 零停机发布：蓝绿双端口(3000/3001) + APISIX route PUT 原子切流 + 旧进程 drain。
# 仓库 deploy/graceful.sh 是唯一正本——make release 每次发布 scp 覆盖到服务器，
# 勿在服务器上手改（会被下次发布覆盖）；应急通道仍是 release-restart.sh（pkill，秒级 502 窗口）。
#
# 不变量：任何时刻 etcd route 指向的 node 都有活着且在 listen 的进程 → 全程零 502。
# 顺序保证：门禁在切 route 之前（新版本起不来/不健康，炸在 green，线上无感）；
#          APISIX 不可达则在第一步就失败（未碰任何进程）。
#
# 与 mistsee 同机共享同一套 APISIX（admin 127.0.0.1:9180，key 在 $DIR/.deploy.env）。
# 依赖：jq、curl、flock、ss(iproute2)、unzip。
# 用法：sh graceful.sh deploy <version> <commit>
#       sh graceful.sh rollback [version]   # 缺省 = .previous_version
#       sh graceful.sh status
set -eu

DIR=/root/newapi/api
REL=$DIR/releases
RUN=$DIR/run
LOGS=$DIR/logs
PORTS="3000 3001"
BIN_NAME=fsdm-new-api
LOG_ROOT=/opt/fsdm/newapi/logs    # --log-dir 根（延续旧链路 /logs/3000 布局）
PKG_ZIP=/root/newapi/build.zip    # make bg-upload 的落点
ADMIN_BASE=http://127.0.0.1:9180
ROUTE_ID=newapi                   # 已核实（2026-11-08）：host=newapi.offiai.cn uri=/* 单 node，插件 proxy-rewrite 由 PUT 原样保留
DOMAIN=https://newapi.offiai.cn
HEALTH_WAIT=60     # green 门禁重试次数（冷启重：AutoMigrate + GetPricing + token encoders + 40 provider）
HEALTH_ITV=1       # 每次间隔秒
SETTLE=5           # 切流后静置：等 APISIX worker 的 etcd watch 传播（默认 delay 1s）
DRAIN_TIMEOUT=360  # 旧进程 SIGTERM 后最多等这么久才 KILL（> .env SHUTDOWN_GRACE=300 + flush/退出余量）
RETAIN=5           # releases/ 保留份数

mkdir -p "$REL" "$RUN" "$LOGS"

# 并发防呆：整段脚本持有（意外退出由内核释放）
exec 9>"$RUN/publish.lock"
flock -n 9 || { echo "✗ 已有发布在跑（$RUN/publish.lock）"; exit 1; }

# APISIX admin key 等敏感项：一次性写入 $DIR/.deploy.env（chmod 600），脚本只 source
# （与 mistsee 的 /root/mistsee/api/.deploy.env 是同一把 key，直接复制过来即可）
[ -f "$DIR/.deploy.env" ] || { echo "✗ 缺 $DIR/.deploy.env（APISIX_ADMIN_KEY=...，chmod 600）"; exit 1; }
. "$DIR/.deploy.env"
: "${APISIX_ADMIN_KEY:?APISIX_ADMIN_KEY 未配置}"

trap 'rm -f "$RUN/route.next.json.tmp"' EXIT

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { log "✗ $*"; exit 1; }

# ---- APISIX：admin API（PATCH 对 nodes 是深合并删不掉旧 node，一律 PUT 全量替换）----
api() { curl -sS -H "X-API-KEY: $APISIX_ADMIN_KEY" "$@" "$ADMIN_BASE/apisix/admin/routes/$ROUTE_ID"; }

route_node() {  # etcd 当前 route 指向的端口（非数字 = route 缺失/结构漂移，即刻止损）
    api | jq -r '.value.upstream.nodes | keys[0]' | sed 's|^127.0.0.1:||' | grep -E '^[0-9]+$' \
        || die "route $ROUTE_ID 读取失败（APISIX/etcd 或结构异常）"
}

switch_route() {  # switch_route <port>：GET 备份 → 改 nodes → PUT → 返回体校验；失败 return 1（green 清理留给调用方）
    want=$1
    api > "$RUN/route.before.json"   # 每次切换前留一份现场
    # {($n): 1} 括号 key 写法：jq 1.7 的 {$n: 1} 简写在 1.6 是语法错误（服务器=1.6 实证）
    if ! jq --arg n "127.0.0.1:$want" \
         '.value | del(.create_time, .update_time) | .upstream.nodes = {($n): 1}' \
         "$RUN/route.before.json" > "$RUN/route.next.json.tmp"; then
        return 1
    fi
    if ! api -X PUT -d @"$RUN/route.next.json.tmp" \
         | jq -e --arg n "127.0.0.1:$want" '.value.upstream.nodes | keys == [$n]' >/dev/null; then
        api > "$RUN/route.after-fail.json"
        return 1   # 现场已存 route.before/after-fail.json，可手工还原
    fi
}

# ---- 端口/进程 ----
busy() { ss -ltn | grep -q ":$1[[:space:]]"; }

peer_port() {  # 与 etcd 当前 node 相反的那个端口（= 下一个 green 的位置）
    cur=$(route_node)
    for p in $PORTS; do
        [ "$p" != "$cur" ] && { echo "$p"; return 0; }
    done
    die "端口对异常：route=$cur PORTS=$PORTS"
}

start_one() {  # start_one <port> <release_dir>
    port=$1; rel=$2
    [ -x "$rel/$BIN_NAME" ] || die "$rel/$BIN_NAME 不存在"
    ln -sfn "$rel/$BIN_NAME" "$DIR/$BIN_NAME$port"   # cmdline 带端口名，pgrep 习惯不破（fsdm-new-api3000/3001）
    cd "$DIR"   # cwd 必须 = $DIR：main.go godotenv.Load(".env") 按工作目录查找
    # 不钉 TZ：沿用旧链路（release-restart.sh 不设 TZ），避免蓝绿进程时区行为漂移。
    # PORT 显式 env 优先于 .env 内同名项：godotenv.Load 不覆盖已存在的环境变量——
    # 这是蓝绿换端口能生效的前提，勿改成写 .env。
    # 9>&- 关闭锁 fd 继承：不加的话服务进程持有 publish.lock 的 fd，脚本退出后
    # 锁仍被占，下次发布死锁（mistsee 首发现场实证）
    nohup setsid env PORT=$port \
        "$DIR/$BIN_NAME$port" --log-dir "$LOG_ROOT/$port" >> "$LOGS/$port.out" 2>&1 9>&- &
    echo $! > "$RUN/server-$port.pid"
}

health_gate() {  # health_gate <port> <commit>：healthz 版本指纹 + readyz DB 连通
    port=$1; commit=$2
    i=0
    while [ "$i" -lt "$HEALTH_WAIT" ]; do
        if curl -sf "http://127.0.0.1:$port/healthz" | grep -q "\"commit\":\"$commit\"" \
           && curl -sf "http://127.0.0.1:$port/readyz" | grep -q ready; then
            return 0
        fi
        sleep "$HEALTH_ITV"; i=$((i + 1))
    done
    return 1
}

stop_one() {  # stop_one <port>：SIGTERM → drain → 超时 KILL；stdout 输出实际 drain 秒数
    port=$1
    pid=$(cat "$RUN/server-$port.pid" 2>/dev/null) || pid=""
    # pid 文件缺失时兜底（首次蓝绿切换：旧链路 release-restart.sh 起的进程没有 pid 文件）。
    # 模式 "/BIN_NAME+端口+空格" 同时匹配旧式 "./fsdm-new-api3000 --log-dir ..." 与本脚本的
    # 绝对路径启动；尾空格防止 3000 误配 3000x，--log-dir 恒存在保证空格在。
    [ -n "$pid" ] || pid=$(pgrep -f "/$BIN_NAME$port " | head -1)
    [ -n "$pid" ] || return 0
    kill -0 "$pid" 2>/dev/null || { rm -f "$RUN/server-$port.pid"; return 0; }
    t0=$(date +%s)
    kill -TERM "$pid"
    while kill -0 "$pid" 2>/dev/null; do
        if [ $(( $(date +%s) - t0 )) -ge "$DRAIN_TIMEOUT" ]; then
            kill -KILL "$pid"; log "!! :$port drain 超 ${DRAIN_TIMEOUT}s 强杀"
            break
        fi
        sleep 2
    done
    rm -f "$RUN/server-$port.pid"
    echo $(( $(date +%s) - t0 ))
}

verify_domain() {  # verify_domain <commit>：443 全链路连续 5 次都得是新版本
    commit=$1
    n=0
    while [ "$n" -lt 5 ]; do
        curl -sf "$DOMAIN/healthz" | grep -q "\"commit\":\"$commit\"" || return 1
        sleep 1; n=$((n + 1))
    done
    return 0
}

cmd=${1:-status}
[ $# -gt 0 ] && shift

case "$cmd" in
deploy)  # graceful.sh deploy <version> <commit>
    ver=$1; commit=$2
    [ -n "$ver" ] && [ -n "$commit" ] || die "用法: graceful.sh deploy <version> <commit>"

    # T0 解包留存（make bg-upload 放到 $PKG_ZIP）。unzip 只写 $DIR/build/ 子树，不会覆盖 $DIR/.env
    if [ -f "$PKG_ZIP" ]; then
        rm -rf "$DIR/build"
        (cd "$DIR" && unzip -oq "$PKG_ZIP")
        mkdir -p "$REL/$ver"
        cp -f "$DIR/build/linux_amd64/$BIN_NAME" "$REL/$ver/$BIN_NAME"
        chmod +x "$REL/$ver/$BIN_NAME"
        echo "$commit" > "$REL/$ver/meta"   # rollback 门禁用
    fi
    [ -x "$REL/$ver/$BIN_NAME" ] || die "releases/$ver 不存在且无 build.zip"

    prev_ver=$(cat "$RUN/.active_version" 2>/dev/null || true)
    cur=$(route_node)
    tgt=$(peer_port)
    busy "$tgt" && die ":$tgt 已被占用（残留进程？先 sh graceful.sh status 排查）"
    log "==> green=:$tgt version=$ver（当前 route=:$cur）"

    # T1-T2 起 green + 门禁：失败只清理 green，route 未动，线上无感
    start_one "$tgt" "$REL/$ver"
    if ! health_gate "$tgt" "$commit"; then
        stop_one "$tgt" >/dev/null
        tail -30 "$LOGS/$tgt.out"
        die ":$tgt 门禁失败（healthz commit / readyz）——route 未动，线上无影响"
    fi
    log "==> :$tgt 门禁通过（commit=$commit）"

    # T3-T4 原子切流 + 静置（慢 worker 此刻拨旧端口仍成功——旧进程还活着）
    switch_route "$tgt" || {
        stop_one "$tgt" >/dev/null
        die "切流失败（route 未动，现场已存 route.before/after-fail.json），green 已清理"
    }
    log "==> route 已切 :$tgt，静置 ${SETTLE}s 等 APISIX worker watch 传播"
    sleep "$SETTLE"

    # T5 443 全链路验证：失败回切 + 清理 green（旧进程在切换前端口 $cur 上）
    if ! verify_domain "$commit"; then
        log "✗ 443 验证失败，回切 :$cur"
        switch_route "$cur"
        stop_one "$tgt" >/dev/null
        exit 1
    fi

    # T6 drain 旧进程；T7 落盘状态 + prune
    drain=$(stop_one "$cur")
    [ -n "$prev_ver" ] && echo "$prev_ver" > "$RUN/.previous_version"
    echo "$ver" > "$RUN/.active_version"; echo "$tgt" > "$RUN/.active_port"
    prev_dir="$REL/$(cat "$RUN/.previous_version" 2>/dev/null || echo x)"
    ls -1dt "$REL"/*/ 2>/dev/null | tail -n +$((RETAIN + 1)) | while read -r d; do
        [ "$(readlink -f "$d")" != "$(readlink -f "$prev_dir")" ] && rm -rf "$d"
    done
    ln -sfn "$LOG_ROOT/$tgt" "$LOG_ROOT/current" 2>/dev/null || true
    log "✓ 发布完成 version=$ver :$tgt drain=${drain:-0}s"
    ;;

rollback)  # graceful.sh rollback [version]
    v=${1:-$(cat "$RUN/.previous_version" 2>/dev/null)}
    [ -n "$v" ] || die "无可回滚版本（$RUN/.previous_version 不存在），用法: rollback <version>"
    commit=$(cat "$REL/$v/meta" 2>/dev/null) || die "releases/$v/meta 缺失（老版本？）"
    cur=$(route_node)
    peer=$(peer_port)

    # 快路径：对偶端口进程还活着且在 listen（上次切换后未退/还没 SIGTERM 的场景）
    p=$(cat "$RUN/server-$peer.pid" 2>/dev/null || true)
    if [ -n "$p" ] && kill -0 "$p" 2>/dev/null && busy "$peer"; then
        cur_ver=$(cat "$RUN/.active_version" 2>/dev/null || echo "")
        switch_route "$peer"
        log "✓ 对偶 :$peer 仍在 serve，已切回（快路径）"
        stop_one "$cur" >/dev/null
        [ -n "$cur_ver" ] && echo "$cur_ver" > "$RUN/.previous_version"
        echo "$v" > "$RUN/.active_version"; echo "$peer" > "$RUN/.active_port"
        exit 0
    fi

    # 慢路径：从 releases/<v> 起在空闲端口 → 门禁 → 切流 → drain 当前
    log "==> rollback → $v（commit=$commit）"
    cur_ver=$(cat "$RUN/.active_version" 2>/dev/null || echo "")
    start_one "$peer" "$REL/$v"
    health_gate "$peer" "$commit" || { stop_one "$peer" >/dev/null; die "回滚版门禁失败"; }
    switch_route "$peer"
    sleep "$SETTLE"
    verify_domain "$commit" || log "!! 443 验证未过，请人工核查（route 已指向 :$peer）"
    drain=$(stop_one "$cur")
    [ -n "$cur_ver" ] && echo "$cur_ver" > "$RUN/.previous_version"
    echo "$v" > "$RUN/.active_version"; echo "$peer" > "$RUN/.active_port"
    log "✓ 已回滚到 $v :$peer drain=${drain:-0}s"
    ;;

status)
    echo "route node  : $(route_node)"
    echo "active      : $(cat "$RUN/.active_port" 2>/dev/null || echo '?') / $(cat "$RUN/.active_version" 2>/dev/null || echo '?')"
    echo "previous    : $(cat "$RUN/.previous_version" 2>/dev/null || echo '?')"
    echo "processes   :"; pgrep -af "$BIN_NAME" || echo "  (none)"
    echo "listen      :"; ss -ltn | grep -E ':(3000|3001)' || echo "  (none)"
    echo "releases    :"; ls -1dt "$REL"/*/ 2>/dev/null | head -5 || echo "  (none)"
    echo "healthz     : $(curl -s "http://127.0.0.1:$(route_node)/healthz")"
    ;;
*)
    die "未知子命令 $cmd（deploy|rollback|status）"
    ;;
esac
