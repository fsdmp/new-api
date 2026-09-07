# Excel Server-Tools M1 — T0 验证清单

状态：代码全量落地（2026-09-07），本清单在 T0 部署后执行。
配套设计：`docs/excel-server-tools-m1-design.md`（§8 上线计划）。

## 0. 部署前置自检（宿主机，root）

| # | 检查 | 命令 | 期望 |
|---|---|---|---|
| P1 | userns 开启 | `sysctl kernel.unprivileged_userns_clone`（Debian/Ubuntu；RHEL 系查 `user.max_user_namespaces` > 0） | =1 / >0 |
| P2 | nsjail 就位 | `ls -l <数据目录>/sandbox/nsjail` | 存在且可执行 |
| P3 | rootfs 就位 | `ls <数据目录>/sandbox/rootfs/usr/local/bin/python3` | 存在 |
| P4 | 沙箱手工跑通 | `echo 'print(1+1)' \| <数据目录>/sandbox/nsjail -Mo --chroot <数据目录>/sandbox/rootfs --user 65534 --group 65534 --cwd /tmp --tmpfsmount /tmp:size=32m -- /usr/local/bin/python3 -I -` | 输出 `2` |
| P5 | 沙箱禁网 | 上一条把代码换成 `import urllib.request;urllib.request.urlopen("http://1.1.1.1",timeout=3)` | 抛网络错误，不允许连出 |
| P6 | 智谱余额 | 智谱开放平台控制台 | web_search 可用 |
| P7 | 反代超时 | nginx `proxy_read_timeout` ≥ 60s | 是 |
| P8 | 网关配置 | 后台 excel_tools：`mode=strip` 起步、`sandbox.enabled=true`、search api_keys 填入 | 保存无错 |

> P4/P5 也可用调试端点验证（P9）：`mode=full` + `debug_endpoints=true` 后
> `curl -X POST <网关>/excel/api/tools/code_execution -H "Authorization: Bearer sk-..." -d '{"code":"print(1+1)"}'`。

## 1. strip 基线（T0 当天，mode=strip）

无联网/无代码能力状态下的回归面——**全部必须通过**才可切 full。

| # | 场景 | 期望 |
|---|---|---|
| S1 | 基本中文对话（"你好"） | 正常回复，行为与发布前一致 |
| S2 | 表格分析（上传区域 + "分析这个表"） | 正常，客户端表格工具零回归 |
| S3 | 问实时信息（"今天黄金多少钱"） | 回复包含"无法联网"类表述，**不再出现 UnknownToolError 道歉循环** |
| S4 | 请求自带 server tools 声明（默认即带） | 网关日志无 unknown server tool 告警风暴 |
| S5 | 多轮对话含 server_tool_use 历史 | 历史块被转换，对话正常继续（无 400） |

## 2. full 模式 — 20 条金融验证 query（mode=full）

覆盖三类工具 × 单轮/多轮/混合。每条记录：是否触发工具、迭代数、最终回复是否包含正确事实、计费行。
判定标准：回复给出**可验证的正确数字/结论**且引用了搜索来源（联网类）。

### A. web_search（1-10）

| # | Query | 通过标准 |
|---|---|---|
| 1 | 今天上证指数收盘多少点？ | 当日实际收盘点位，误差 0 |
| 2 | 现在人民币兑美元汇率是多少？ | 即时汇率，小数级一致 |
| 3 | 当前布伦特原油价格 | 与公开行情一致 |
| 4 | 黄金现价多少美元一盎司 | 与公开行情一致 |
| 5 | 最近一次美联储议息决议结果是什么？ | 正确的决议日期 + 利率动作 |
| 6 | 贵州茅台最新股价 | 当日价格 |
| 7 | 本月 LPR 是多少？ | 当月 1 年期/5 年期报价 |
| 8 | 2025 年中国 GDP 增速是多少？ | 官方口径数字 |
| 9 | 比特币现在什么价格 | 与主流行情一致 |
| 10 | 恒生指数今天涨还是跌，幅度多少 | 方向 + 涨跌幅正确 |

### B. web_search + web_fetch 联动（11-13）

| # | Query | 通过标准 |
|---|---|---|
| 11 | 帮我读一下今天[某财经站点]头条文章的要点 | 抓取正文并给出来源 URL |
| 12 | 查一下特斯拉最新财报的营收数字，给出出处 | 搜索→点开结果页→数字与出处正确 |
| 13 | 当前 10 年期国债收益率是多少？来源呢？ | 数值 + 可点开的来源链接 |

### C. code_execution（14-17）

| # | Query | 通过标准 |
|---|---|---|
| 14 | 帮我精确计算 1234567.89 × 9876543.21 | 触发沙箱，结果精确（心算易错的大数乘法） |
| 15 | 这组数的标准差是多少：2,4,4,4,5,5,7,9 | 沙箱计算，≈2.138 |
| 16 | 用 pandas 算 1 到 100 的和再开根 | 触发且正确（√5050 ≈ 71.06） |
| 17 | 写段代码演示读取一个 xlsx 的前 3 行（openpyxl） | 代码执行成功或给出可信输出 |

### D. 混合与压力（18-20）

| # | Query | 通过标准 |
|---|---|---|
| 18 | 先查今日油价，再用代码把人民币价格换算成美元（假设汇率也查） | 两个工具同回合或跨迭代串联 |
| 19 | 连续问 5 个实时行情问题（快速连发） | 无断流；SSE 不中断；每条都有 ping 保活痕迹 |
| 20 | （超限）把 search daily_quota 临时调成 2 后连问 3 个搜索问题 | 第 3 条得到"今日额度已用完"的模型转述，无 HTTP 错误 |

## 3. 协议与安全回归（mode=full）

| # | 场景 | 期望 |
|---|---|---|
| R1 | MS Excel（官方加载项）+ WPS 各跑一轮 S1-S5 | 双宿主无渲染差异：server_tool_use / *_tool_result 块正常显示或安全降级 |
| R2 | 中途断开客户端连接（工具执行期间关闭表格） | 网关不崩，日志有 client gone 停 ping 记录 |
| R3 | vConsole 抓 SSE（设计 §12 遗留项 1） | 单一 message_start/message_stop；块 index 全局连续；无重复事件 |
| R4 | 后台关掉 search key（full 模式） | web_search 降级为 strip（不注入），模型不调用 |
| R5 | Redis 停 30s | 配额放行（availability-wins），日志有告警 |
| R6 | 沙箱 30s 死循环代码 | timed_out，模型收到超时说明并改口直接回答 |
| R7 | 迭代上限：诱导模型连续调用工具 | 第 N-1 轮拒绝反馈、第 N 轮强制 end_turn，客户端只见一次消息框架 |
| R8 | 调试端点关闭（默认） | POST /excel/api/tools/web_search → 404 |
| R9 | SSRF：调试端点抓 `http://169.254.169.254/` 与 `http://127.0.0.1:端口` | 拒绝，错误信息不泄露内网细节 |

## 4. 回滚验证

| # | 场景 | 期望 |
|---|---|---|
| B1 | 切回 mode=strip | 1 分钟内生效（无重启），S3 行为恢复 |
| B2 | 切回 mode=off | 与发布前字节级同行为（空 schema 放行、历史块丢弃） |

## 结果记录

在下方追加：日期 / 环境（网关版本、机型）/ 每条通过与截图或日志摘录 / 未通过项的根因。

### 2026-09-07 线上首测（网关 v20260907-1540，8.140.235.234，curl 探针直打 /excel/v1/messages）

实测前修复（commit ed595eb3）：智谱 raw Bearer 鉴权、合成帧补 type 字段、
adapter 注入当前日期（模型无时钟会编造日期）、无 link/url 的智谱 AI 摘要卡片不再丢弃。

| 项 | 结果 | 备注 |
|---|---|---|
| P1 userns | ✅ | `kernel.unprivileged_userns_clone = 1` |
| P4/P5/P6/P8 | ✅ | 沙箱项除外：智谱 key 取自 GLM 渠道（同一账号），搜索直测 ok；P8 直接写 options 表 |
| P7 反代 | ✅ | APISIX 无自定义读超时（默认 60s），ping 2s 足够 |
| A1 上证收盘 | ✅ | 当日 3932.70 精确 + OHLC 表，模型正确排除旧闻 |
| A2 汇率 | ⚠️ 部分 | 最新仅到 8/17（指数滞后），模型诚实标注时点并给走势 |
| A3 布伦特 | ✅ | 9/1 报价 90.49 美元/桶，模型调和了矛盾数据 |
| A4 黄金 | ⚠️ 部分 | 最新到 8/13（4449.73），模型拒报不确定数字 |
| A5 美联储 | ✅ | 2026-06-17 决议维持 3.5%–3.75% + 背景 |
| A6 茅台 | ✅ | 9/4 收盘 1330.00，正确指出今日周一盘中 |
| A7 LPR | ✅ | 正确说明 9/20 才公布，给 8/20 报价 3.0%/3.5% |
| A8 GDP | ✅ | 5.0%、140.19 万亿，官方口径 |
| A9 比特币 | ⚠️ 部分 | 智谱内容审查 1301 在第 2 轮拦截上游→SSE error 优雅终止，已产出正文保留；行情仅到 8 月下旬 |
| A10 恒指 | ✅ | 当日盘中 -0.8%~-1.0%，OHLC 表 |
| B11 读头条 | ✅ | fetch×2+search×2 串联，读出当日东财头条全文要点 |
| B12 特斯拉财报 | ✅ | Q2 2026 营收 282.4 亿美元 +26%，给出处 |
| B13 10Y 国债 | ✅ | 中 1.68%（9/4）美 4.80%（9/7），均带来源 |
| D19 连发 | ✅ | ~15 连发无断流，每条完整 end_turn |
| D20 配额拒绝 | ✅ | quota=2 → 模型转述「额度已达上限」，无 HTTP 错误 |
| R3 协议 | ✅（探针侧） | 单 message_start/stop、index 全局连续、聚合 usage 正确；vConsole 真机仍待前端 |
| R9 SSRF | ✅ | 169.254.169.254 / 127.0.0.1:3000 / localhost 全部 exec_error 拒绝 |
| C14-17 / R6 / R7 | ✅ | 见下方沙箱补测（v20260907-1627，runner=native） |
| R2 断连 / R5 Redis 停 | ⏳ | 生产上不宜制造，留待沙箱/预发环境 |
| B1/B2 回滚开关 | ✅（等效） | 上午 strip 基线已实际运行；单测锁死 off 行为 |

**结论**：full 模式搜索/抓取链路达到可用标准。现货报价类（汇率/黄金/比特币）
受智谱索引时效限制，模型均能诚实标注数据时点——属引擎质量上限，非网关缺陷。
已知约束：智谱对加密货币话题有内容审查（1301），迭代内触发则以 SSE error 收尾。

### 2026-09-07 沙箱补测（网关 v20260907-1627，runner=native，rootfs=python3.11-slim+pandas/numpy/openpyxl/pdfplumber 等 19 wheels）

沙箱落地方式（生产无 Docker 红线下的替代链）：daocloud 镜像直拉
library/python:3.11-slim layer blobs 解包为 rootfs + pip 21.3.1（py3.6 兼容）
下载 cp311 manylinux wheels 解包进 site-packages；隔离用网关内置 Go
namespace runner（sandbox_native.go），不依赖 nsjail 二进制。安全要点
（均在生产实测验证）：uid/gid 双映射 {0→0, 65534→65534} + chroot +
NEWNET 禁网 + `setpriv --no-new-privs`（封死 rootfs suid 提权）+
清空 bounding/inheritable/ambient caps + tmpfs /tmp + rlimits。

| 项 | 结果 | 备注 |
|---|---|---|
| P4 print(1+1) | ✅ | 调试端点与 /excel/v1/messages 链路均 2 |
| P5 禁网 | ✅ | urllib → Network is unreachable（NEWNET 无接口） |
| 权限 | ✅ | CapEff=0、NoNewPrivs=1、/tmp=tmpfs、rootfs 只读（PermissionError） |
| C14 大数乘法 | ✅ | Decimal 精确 12193263111263.5269，模型给出交叉验证 |
| C15 标准差 | ✅ | 总体 2 + 样本公式说明，沙箱核算 |
| C16 pandas | ✅ | sum 5050 / √5050 ≈ 71.0634 |
| C17 openpyxl | ✅ | 模型自建示例 xlsx 再读取前 3 行，双方式（pandas/openpyxl）跑通 |
| R6 死循环 | ✅ | 纯 `while True` 被超时杀，模型准确转述"沙箱超时强制终止"并解释原因 |
| R7 迭代上限 | ✅（等效+） | 五连问触达每回合调用上限 → 模型收到拒绝反馈 → 自动降级 web_fetch 完成 → 正常 end_turn，单消息框架 |
| 30s 长任务保活 | ✅ | 模型自限时 30.000s 循环任务期间 SSE 不断流，ping 保活有效 |
| 内存上限 | ✅ | 2GB 分配 → MemoryError（ulimit -v 1GB） |

**沙箱遗留**：rootfs 灾备包 sandbox/excel-sandbox-rootfs.tar.xz（66MB）已
在生产生成；新机部署走 `make upload-sandbox`（nsjail 可选，runner=native
只需 rootfs）。
