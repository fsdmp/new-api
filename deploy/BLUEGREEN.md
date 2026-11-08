# new-api 零停机蓝绿发布（newapi.offiai.cn · 8.140.235.234）

部署形态：裸二进制（无 Docker / 无 systemd），APISIX（与 mistsee 同机共享同一套）443 反代 →
本机 3000/3001 蓝绿双端口。发布走 `make release` → `deploy/graceful.sh`（**仓库正本**，
每次发布 scp 覆盖服务器，勿在服务器手改）。

核心不变量：**任何时刻 etcd route 指向的端口都有活着且在 listen 的进程 → 全程零 502**。
顺序保证：门禁（healthz commit 指纹 + readyz DB ping）在切 route 之前——新版本起不来/
不健康炸在 green，线上无感；APISIX 不可达则脚本第一步就失败（未碰任何进程）。

流程（T0-T7）：解包到 `releases/<ver>/` → 对偶端口起 green → 门禁（最长 60s）→
APISIX admin API **PUT 全量替换** route nodes 原子切流 → 静置 5s 等 etcd watch 传播 →
443 全链路连续 5 次命中新 commit → 旧进程 SIGTERM drain（`SHUTDOWN_GRACE`，生产 .env 已设 300s，
代码默认 180s）在途 SSE 流完整跑完后 flush 批量配额缓冲再退 → 落盘状态 + prune（releases/ 保 5 份）。

```bash
make release         # 日常零停机发布（蓝绿切换 + 443 验证）
make rollback        # 回滚上一版本（秒级快路径：对偶端口进程还在就直接切回）
make rollback ARGS=v20261108-1030   # 回滚到指定版本（慢路径：从 releases/<v>/ 起进程）
make release-status  # route 指向 / 活跃版本端口 / 进程 / releases / healthz
make restart-service # 【应急】旧式 pkill 重启（秒级 502 窗口，仅救援/兜底，见下文 runbook）
```

## 服务器实况核实（2026-11-08，对齐依据）

| 项 | 实况 |
|---|---|
| 进程 | `./fsdm-new-api3000 --log-dir /opt/fsdm/newapi/logs/3000`，cwd=`/root/newapi/api`，无外部 env（配置全靠 cwd 的 `.env`，godotenv 加载；无 TZ 注入） |
| ROUTE_ID | **`newapi`**（host=newapi.offiai.cn，uri=`/*` 单 node 127.0.0.1:3000，插件 proxy-rewrite——PUT 全量替换会原样保留） |
| admin API 形状 | 列表接口返回 `{total, list[]}`（不是 `{data[]}`）；route 字段是单数 `host`/`uri`。**admin key 与 mistsee 同一把**（/root/mistsee/api/.deploy.env） |
| 安全组 | 外网实测 3000/9998/9180 全部超时（默认拒绝）→ green 绑 `*:3001` 无暴露风险 |
| 端口 | 3000 在听（`*:3000` 全网卡）；**3001 空闲**；9998/9999 为 mistsee 蓝绿占用 |
| .env 关键值 | `PORT=3000`（蓝绿靠 start_one 显式 env PORT 覆盖，godotenv 不覆盖已存在 env）；`BATCH_UPDATE_ENABLED=true`（flush 是生产真实路径）；`MEMORY_CACHE_ENABLED=true` + Redis 已配（双进程共享缓存，蓝绿一致性友好）；`RELAY_TIMEOUT=600`（在途请求硬顶 10 分钟）→ 已追加 `SHUTDOWN_GRACE=300`（备份 .env.bak-bluegreen）；`STREAMING_TIMEOUT=300`；`GIN_MODE=release` |
| 依赖 | jq 1.6 ✓ curl/flock/ss/unzip ✓；磁盘 20G 可用（releases ×5 ≈ 0.5G）✓ |
| 杂项 | `/root/newapi/api/build.zip` 是 8 月旧产物（graceful.sh 读的是 `/root/newapi/build.zip`，路径不同互不相干）；`output.log` 已 488MB 未轮转（首次蓝绿切换后 release-restart.sh 退役，轮转机制随之消失——建议低峰手动归档一次） |

## 一次性准备（已于 2026-11-08 全部完成 ✓）

以下为已完成记录（换机/重置后照此重做）：

1. **查 route id**（与 mistsee 共用同一把 admin key）：
   ```bash
   KEY=$(sed -n 's/^APISIX_ADMIN_KEY=//p' /root/mistsee/api/.deploy.env)
   curl -s -H "X-API-KEY: $KEY" http://127.0.0.1:9180/apisix/admin/routes \
     | jq -r '.list[] | [.value.id, .value.host, .value.uri, (.value.upstream.nodes|keys|join(","))] | @tsv'
   ```
   取 node 为 `127.0.0.1:3000` 的 route id → **newapi**（已填入 graceful.sh）。
2. **`.deploy.env`**：已创建 `/root/newapi/api/.deploy.env`（APISIX_ADMIN_KEY，600 权限）。
3. **底牌备份**：已备份 `/root/newapi/api/run/route-baseline.json`。
   手工还原命令（应急）：`jq '.value | del(.create_time, .update_time)' run/route-baseline.json > /tmp/r.json && curl -s -X PUT -H "X-API-KEY: $KEY" http://127.0.0.1:9180/apisix/admin/routes/newapi -d @/tmp/r.json`
4. **安全组**：外网实测已全拦（见上表）。
5. **依赖**：jq 1.6 / curl / flock / ss / unzip 全部就绪。
6. **route uri 覆盖**：uri=`/*` 已覆盖 `/healthz` `/readyz`，无需补。
7. **磁盘**：20G 可用，充足。
8. **.env 追加 `SHUTDOWN_GRACE=300`**（备份 .env.bak-bluegreen）——对齐 RELAY_TIMEOUT=600 的 drain 折中；graceful.sh `DRAIN_TIMEOUT=360` 与之配套。

## 首次切换预期（只发生一次）

- 现网旧二进制**没有优雅关停**：首次 deploy 的 T6 SIGTERM 会把旧进程即刻打死，
  其在途请求被硬切一次（新连接此时早已在 green 上，无 502）。**选低峰执行**。
- 旧进程没有 pid 文件——`stop_one` 里的 pgrep 兜底（`pgrep -f "/fsdm-new-api3000 "`）
  负责找到并停掉它。
- 首次成功后 `run/.active_port=3001`，旧链路的 `.prev/.new/.bad` 文件作废。
- **紧接着做一次演练**（无代码变化，10 分钟内把所有分支练一遍）：
  再 `make release`（这次旧进程带优雅关停，验证真 drain 路径：在途流完成、无 KILL）→
  `make rollback`（验证快路径）→ 再 `make release` 切回。

## 运维要点

- **双跑窗口**（green 起到旧进程退出，约 20-60s）：SyncOptions / UpdateQuotaData /
  渠道测试 / 任务轮询在两进程同时跑——看板统计可能轻微重复、渠道测试翻倍；
  资金路径（配额扣减）有 CAS 保护 + 关停 flush 顺序保证，不受影响。
- **WS 断连语义**：/v1/realtime（WebSocket，hijacked 连接）不被 http.Server.Shutdown
  等待，随旧进程退出断开，客户端重连——与旧链路行为一致，非回退。
- **schema 迁移**：green 启动时带旧进程在场跑 GORM AutoMigrate。**随蓝绿发布只允许
  additive 迁移**（加表/加列）；破坏性变更（删列/改类型/建唯一索引收紧张）走
  `make restart-service` 维护窗口。
- **pprof**：ENABLE_PPROF=true 时固定绑 0.0.0.0:8005，双跑窗口第二个进程绑失败仅记
  日志不致命；窗口内 pprof 只对先起的进程可用。
- **drain 对账**：发布日志关注 `drain=Ns`；若达 DRAIN_TIMEOUT(360s) 强杀，配额损失
  上限 = 一个 BATCH_UPDATE_INTERVAL(5s) 的缓冲增量，且逐请求计费已在日志库落库可对账。
  剩余时长 >SHUTDOWN_GRACE(300s) 的流会被截断（RELAY_TIMEOUT=600 硬顶下属罕见）——
  发布选低峰；流量需要时可调大 `.env` 的 `SHUTDOWN_GRACE`（秒），
  脚本 `DRAIN_TIMEOUT` 需同步 > grace + 60。
- **服务器重启后**：route 仍指向死端口，线上 502——**重启后第一件事就是 `make release`**
  （自动在空闲侧拉起并切流，即自愈）。
- **同版本号重发会覆盖 `releases/<VERSION>/` 且把 `.previous_version` 污染成当前版本**——
  回滚素材失真；需要回滚时用 `make rollback ARGS=<明确版本号>`。
- **发布失败现场**：`run/route.before.json`（每次切换前自动留档）、
  `run/route.after-fail.json`（切流失败时）、`run/route-baseline.json`（底牌）。

## 【应急】runbook（仅救援/兜底：服务器起不来、APISIX 失联、门禁反复失败等）

`make restart-service` 起的是 `REMOTE_BIN`（缺省 3000 端口）进程，**与蓝绿状态可能错位**：

1. 先 `make release-status` 看 active 端口；
2. 若 active=3001，应急链要用：
   `make upload REMOTE_BIN=fsdm-new-api3001` + `make restart-service REMOTE_BIN=fsdm-new-api3001 REMOTE_START_ARGS="--log-dir /opt/fsdm/newapi/logs/3001"`
   并保证 PORT=3001 传到进程（`.env` 临时加一行 `PORT=3001` 最稳）；
3. 应急恢复后**尽快跑一次 `make release`** 重建 `run/` 状态文件，回到蓝绿正轨。
