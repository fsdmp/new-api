# Excel 网关 Server Tools（M1）技术设计

> 日期：2026-09-07 · 状态：**待评审**
> 前置文档：`~/Downloads/capability-gap-analysis.md`（能力差距分析，§9 服务端需求清单）
> 结论已定：采用**方案 A（网关内工具闭环）**替代原文档的方案 B，理由见 §4.1。

---

## 1. 目标与非目标

### 目标

在 `/excel/v1/messages` 单一端点内，为三种 server tool（`web_search` / `web_fetch` / `code_execution`）提供**网关内闭环执行**：

1. **S1**：`sanitizeExcelTools` 按工具 `type` 前缀注入真实 schema + description；
2. **工具循环**：网关拦截模型对三种虚拟工具的调用 → 网关执行 → 结果回注 → 模型继续生成，直到自然结束；
3. **SSE 合成**：对客户端呈现为单次标准 Anthropic Messages 流式响应，含原生 `server_tool_use` / `*_tool_result` 块（状态行"搜索了网页 / 运行了 N 个脚本"由客户端原生渲染）；
4. **历史转换**：客户端后续请求回传的 `server_tool_use` / `*_tool_result` 块转换为 `tool_use` / `tool_result` 喂给上游模型；
5. **执行器**：web_search（供应商抽象）、web_fetch（SSRF 安全抓取）、code_execution（Docker 沙箱）；
6. 按次计费与防滥用配额（默认价格 0，机制就位）。

### 非目标（明确不做）

- 不改 `/v1/*` 及其他任何非 Excel 路由的行为；
- 不新增任何不在 `/excel/*` 下的端点；
- 不改 `service/convert.go`、`relay/claude_handler.go`、`controller/relay.go` 等共享管线代码；
- 沙箱 INPUT_DIR 文件联动（等 S3 文件服务，接口预留）；
- 会话级容器保活 / 预热池（二期优化）；
- 图片 / 文件上传（M2）、语音（M3）、MCP（M4）、技能云同步（M5）。

---

## 2. 设计红线

| # | 红线 | 来源 |
|---|---|---|
| R1 | 所有新增 HTTP 端点必须挂在 `/excel/*` 路由组下 | 本项目约束 |
| R2 | **共享 relay 管线零改动**：`controller/relay.go`、`relay/claude_handler.go`、`service/convert.go`、计费代码不修改；非 Excel 流量零影响 | 本项目约束 |
| R3 | `/excel/v1/messages` 对外协议保持 Anthropic Messages + SSE 不变 | 前端差距分析 §9.8-1 |
| R4 | 沙箱隔离三原则不让步：容器禁出网、根文件系统只读、一次性执行 | 前端差距分析 §9.8-6 |
| R5 | 配置走 new-api 现有配置体系（`setting/config.GlobalConfig`），不硬编码 | 前端差距分析 §9.8-5 |
| R6 | 遵守仓库 CLAUDE.md：JSON 用 `common.Marshal/Unmarshal`；DB 兼容三种数据库；relayed DTO 可选字段用指针 | 仓库规范 |

---

## 3. 总体架构

### 3.1 组件图

```
客户端（Claude for Excel bundle，任意版本）
   │  POST /excel/v1/messages  (tools 含 {type:"web_search_20250305", name:"web_search", ...})
   ▼
router/excel-gateway-router.go ── POST /messages → controller.ExcelMessagesRelay（新）
   │
   ├─ 不含虚拟工具 / flag=off ──→ controller.Relay(c, RelayFormatClaude)   【零成本路径，行为与现状逐字节一致】
   │
   └─ 含虚拟工具 且 flag=full ──→ ToolLoop（新，编排器）
        │
        │   ┌──────────────── 迭代 i = 1..N（默认 ≤5）────────────────┐
        │   │  1. 构造/扩展 body storage（i>1 时追加 tool_use+tool_result）│
        │   │  2. c.Writer ← StreamInterceptor（SSE 改写器，新）          │
        │   │  3. controller.Relay(c, RelayFormatClaude)  ← 完整管线复用： │
        │   │      解析 → 估价 → PreConsume → 渠道选择/重试 → 上游 →       │
        │   │      OpenAI SSE → Claude SSE（convert.go 现有转换）→ c.Writer│
        │   │  4. 拦截器输出：透传(索引重写后)事件 + 捕获的虚拟工具调用     │
        │   │  5. 有虚拟调用 → ExecutorRegistry 执行（期间发 ping 保活）    │
        │   │     → 合成 server_tool_use + *_tool_result 块（全局索引）    │
        │   │  6. 无虚拟调用 / 有客户端工具 / 达上限 → 收尾终止             │
        │   └──────────────────────────────────────────────────────────┘
        ▼
   StreamInterceptor：对真实客户端 writer 输出单条连续的 Anthropic SSE 流
```

### 3.2 关键机制复用（已读码确认）

| 机制 | 位置 | 复用方式 |
|---|---|---|
| body 可重写 | `middleware/excel_adapter.go:140-148` 设置 `common.KeyBodyStorage`；`controller/relay.go:200-210` 每次调 Relay 重读 | 迭代 N 重写 body storage 后再次调 Relay，天然生效 |
| 渠道继承 | `getChannel`：`info.ChannelMeta == nil` 时读 Distribute 中间件写入的 context 渠道（`controller/relay.go`） | 同一请求内迭代重入 Relay 自动复用首次分发渠道，且保留渠道级重试 |
| OpenAI→Claude SSE 转换 | `service.StreamResponseOpenAI2Claude`（`service/convert.go:255`），索引状态在每次 Relay 调用各自的 RelayInfo 上 | 每轮迭代索引从 0 开始，改写器统一加偏移 |
| Claude 请求校验 | `GetAndValidateClaudeRequest` → `UnmarshalBodyReusable` | 每轮重读 body storage，无需改动 |
| 按次工具计费 | `service/tool_billing.go` `ComputeToolCallQuota` + `operation_setting.GetToolPrice` | 工具调用计费复用同一价格配置体系 |
| 配置注册 | `setting/config.GlobalConfig.Register`（`setting/excel_setting/model_list.go:27` 先例） | 新增 `excel_tools` 配置组 |

---

## 4. 关键决策记录

### 4.1 为什么是方案 A（网关闭环）而非原文档的方案 B

| 维度 | 方案 B（REST + 客户端执行器） | 方案 A（本设计） |
|---|---|---|
| 前端依赖 | vendor 逆向 `X5` 注册表注入执行器（2-3 天，最脆弱环节，vendor 升级即重做） | 仅取消隐藏入口（半天） |
| 存量客户端 | 必须 upgrades 后可用，老版本永远 UnknownToolError | **全版本立即受益，零升级** |
| 发布时序 | S1 schema 注入必须与前端执行器同窗口（否则道歉循环加剧） | 网关内部 flag 控制，无耦合 |
| 服务端工作量 | 小（3 个无状态 REST） | 大（本设计，12-14.5 天） |
| 失败模式 | 客户端 UnknownToolError（未注册时） | 网关内优雅降级（错误喂回模型） |

结论：方案 A 用服务端复杂度换掉前端风险与版本耦合，且执行器内核两个方案同构，投资不亏。

### 4.2 循环层挂载点：路由闭包 + ResponseWriter 拦截（而不是改 ClaudeHelper）

候选对比：

| 方案 | 结论 |
|---|---|
| 改 `relay/claude_handler.go` 在 DoResponse 后循环 | ❌ 违反 R2（共享代码） |
| 复制一份 `ExcelClaudeHelper` 带循环 | ❌ ~200 行共享逻辑分叉，漂移风险 |
| **路由闭包内编排，迭代重入 `controller.Relay`，Writer 层拦截 SSE** | ✅ 共享代码零改动；每轮迭代自动继承鉴权后的完整能力（渠道重试、failover、状态码映射、计费） |

代价与对策：
- 拦截器需正确处理 gin.ResponseWriter 委托（实现接口转发全部方法）；
- 每轮独立计费（见 §4.4）；
- 迭代 ≥2 上游失败时 Relay 会向 writer 写 JSON 错误而非 SSE——拦截器检测"流已开始后出现非 SSE 输出"转译为 SSE `error` 事件（见 §5.5）。

### 4.3 终止语义与混合工具调用

规则（与 Anthropic 原版语义对齐）：

1. **继续条件**：本轮含 ≥1 个虚拟工具调用、且**无**客户端工具调用、且迭代 < N；
2. **客户端工具终止**：本轮出现客户端工具 `tool_use`（`set_cell_range` 等）→ 终止循环。本轮的虚拟工具调用**仍执行**、合成块**仍下发**（客户端可见结果），但结果不回喂模型——下一轮客户端请求的历史带回这些块，经历史转换后模型自然可见；
3. **迭代上限**：达到 N（默认 5）后，最后一轮的 tool_result 以文本 `"本回合该工具调用额度已用完，请基于已有信息直接回答"` 拒绝执行，强制模型以 `end_turn` 收尾——保证流永不半截；
4. `max_uses`（如 `web_search` 的 1）：编排器按请求内工具定义的 `max_uses` 计数执行，超出同上以文本拒绝。

### 4.4 计费模型：每轮独立结算 + 工具按次另计

- 每轮迭代 = 一次完整 `controller.Relay` 调用 = 一次独立的 PreConsume → PostConsume 流转（现有代码，零改动）。**账单粒度：一轮一行**（同一 request_id）。这是诚实口径（每轮真实消耗上游 token）；ops 需知悉"一次带工具的对话 = 多行账单"。二期可做聚合展示。
- 用户额度中途不足：第 N 轮 PreConsume 失败 → Relay 写 JSON 错误 → 拦截器转 SSE error 终止。语义正确（额度耗尽）。
- 工具按次计费：循环结束后编排器调用新函数 `PostExcelToolConsumeQuota`（复用 `ComputeToolCallQuota` 的价格解析与 quota 换算，写独立账单行，content 标记 `excel-server-tools`）。**默认价格 0（不计费），机制与配置先行**。实现时对齐 `PostTextConsumeQuota` 的模式与 `relayInfo.Billing` 的预扣关系，此为唯一实现期确认点。

### 4.5 合成块形状的适配层

`server_tool_use` / `web_search_tool_result` / `web_fetch_tool_result` / `code_execution_tool_result` 的字段形状以 Anthropic 公开文档为初始实现。**前端 vendor 的真实解析形状未经原版环境验证**，故形状渲染隔离在 `RenderResult` 单点（见 §5.6），联调不符时只改 renderer。降级保证：形状不符的最坏后果是状态行不渲染，**功能本身不受影响**（结果以文本形式进入模型续写与最终回答）。

---

## 5. 模块详细设计

### 5.1 配置与开关 `setting/excel_tools/`（新包）

注册 `excel_tools` 配置组（镜像 `excel_model_list` 模式）：

```go
type ToolsSettings struct {
    Mode          string `json:"mode"`           // "off" | "strip" | "full"（默认 strip）
    FullUserIds   []int  `json:"full_user_ids"`  // 按用户提前放行 full（内测灰度）

    Search struct {
        Provider         string `json:"provider"`          // 默认 "zhipu"（2026-09-07 定：单家）
        FallbackProvider string `json:"fallback_provider"` // 备供应商（可空 = 不降级；预留扩家）
        // 按请求路由后的模型名前缀选供应商（如 {"glm-": "zhipu"}）；
        // 无命中回退 Provider。注意 DeepSeek 平台无搜索 API，deepseek-* 前缀
        // 目前也只能映射到智谱——机制在，扩家（如火山引擎）后配置即生效。
        ProviderByModelPrefix map[string]string `json:"provider_by_model_prefix"`
        APIKeys          map[string]string `json:"api_keys"` // {"zhipu": "..."}
        MaxResults int   `json:"max_results"`    // 默认 5，硬上限 10
        DailyQuota int   `json:"daily_quota"`    // per-user 次/日，默认 50
    }
    Fetch struct {
        TimeoutSec    int   `json:"timeout_sec"`     // 默认 15
        MaxBodyBytes  int64 `json:"max_body_bytes"`  // 默认 5MB
        DailyQuota    int   `json:"daily_quota"`     // 默认 100
    }
    Sandbox struct {
        Enabled       bool   `json:"enabled"`
        NsjailPath    string `json:"nsjail_path"`    // 默认 <数据目录>/sandbox/nsjail
        RootfsPath    string `json:"rootfs_path"`    // 默认 <数据目录>/sandbox/rootfs
        TimeoutSec    int    `json:"timeout_sec"`    // 默认 30
        MaxConcurrent int    `json:"max_concurrent"` // 全局并发，默认 4
        DailyQuota    int    `json:"daily_quota"`    // 默认 50
    }
    Loop struct {
        MaxIterations int `json:"max_iterations"`    // 默认 5
    }
    Prices struct {
        WebSearchPer1k      float64 `json:"web_search_per_1k"`      // 默认 0
        WebFetchPer1k       float64 `json:"web_fetch_per_1k"`       // 默认 0
        CodeExecutionPer1k  float64 `json:"code_execution_per_1k"`  // 默认 0
    }
    DebugEndpoints struct {
        Enabled bool `json:"enabled"`   // /excel/api/tools/* 调试端点开关，默认 false
    }
}
```

模式语义：

| mode | sanitizeExcelTools 行为 | 循环 | 用途 |
|---|---|---|---|
| `off` | 现状（空 schema 放行） | 否 | 完全回滚到发布前行为 |
| `strip`（默认） | **剥掉**虚拟工具条目 | 否 | 安全态：模型看不到这些函数，UnknownToolError 道歉循环立即消失；同时在中文注入段追加一句"当前无联网搜索/代码执行能力"降级提示（与 mode 联动，full 下不注入，抵消 vendor 提示词吹嘘残留） |
| `full` | 注入真实 schema + description | 是 | 完整能力 |

判定优先级：请求用户 ∈ `FullUserIds` → full；否则按全局 mode。执行器自身不可用（如未配搜索 key、沙箱未启用）时，该工具条目降级为 strip（不让模型调用一个必定失败的工具）。

### 5.2 S1：schema 注入 `middleware/excel_adapter.go`

`sanitizeExcelTools`（:274-304）修改——按 `toolMap["type"]` **前缀**匹配（兼容未来版本后缀漂移，如 `web_search_2026xxxx`）：

```go
// 伪代码
if prefixes, ok := excel_tools.MatchVirtualTool(typeStr); ok {
    if !excel_tools.VirtualToolActive(name, userId) { return nil /* strip */ }
    return append(result, excel_tools.ToolDefinition(name)) // {name, description, input_schema}
}
// 其余工具：现有逻辑不变
```

三种工具的 schema 单一事实来源在执行器定义里（§5.6），`excel_adapter` 引用之，避免两处维护：

| name | description（引导 DeepSeek 正确传参） | required |
|---|---|---|
| `web_search` | Search the web for current information (prices, news, recent events). Returns title/url/snippet. Use at most once per turn unless necessary. | `query` |
| `web_fetch` | Fetch a web page by absolute http(s) URL and return its main content as markdown. Use to read pages found via web_search. | `url` |
| `code_execution` | Run a Python script, return stdout/stderr. pandas/numpy/openpyxl/pdfplumber/python-docx preinstalled. Use for precise computation/statistics instead of mental math. | `code` |

未知 server tool 前缀（既非三种之一又无 `input_schema`）：strip + `SysLog` 告警（vendor 升级探测哨兵，呼应差距分析 §10.4-1）。

**同文件顺带修复**：`:138` 全量 body 日志改为截断 2KB + 配置开关（差距分析 §9.8-4 要求的脱敏评估）。

### 5.3 历史块转换 `middleware/excel_adapter.go`

`sanitizeExcelContentBlocks`（:224-246）：白名单外的三种历史块**转换而非丢弃**（字段形状兼容，仅改 type + 保留字段）：

| 客户端回传块 | 转换为 | 说明 |
|---|---|---|
| `server_tool_use` `{id, name, input}` | `tool_use` | 字段同名同义，仅改 type；`ClaudeToOpenAIRequest` 既有 tool_use 分支直接消费 |
| `web_search_tool_result` / `web_fetch_tool_result` / `code_execution_tool_result` `{tool_use_id, content}` | `tool_result` | content 数组由既有 tool_result 分支 JSON 序列化喂模型 |

注意：仅当 `mode=full` 时转换（strip 模式下历史里出现这些块说明来自 full 时期，转换无害，统一转换即可）。`server_tool_use` 加入 `excelSupportedBlockTypes` 的处理要小心——不能简单加白名单（会导致原样透传给上游，上游不认识），必须在转换分支内处理。

### 5.4 循环编排器 `controller/excel_tool_loop.go`（新）

```go
func ExcelMessagesRelay(c *gin.Context) {
    plan := exceltools.BuildLoopPlan(c)   // 读 body storage + 配置，产出：是否激活、虚拟工具集、max_uses 映射
    if !plan.Active {
        Relay(c, types.RelayFormatClaude)  // 零成本路径
        return
    }
    runToolLoop(c, plan)                   // 不可恢复的内部错误 → 降级调用 Relay(c, ...)
}

func runToolLoop(c *gin.Context, plan *LoopPlan) {
    loop := newLoopState(plan)             // 聚合 usage、全局块索引 offset、message id
    body := currentBody(c)
    for iter := 1; iter <= plan.MaxIterations; iter++ {
        setBodyStorage(c, body)                        // 复用 excel_adapter 的 body 替换机制
        interceptor := newStreamInterceptor(realWriter, loop, plan)
        swapWriter(c, interceptor)
        Relay(c, types.RelayFormatClaude)              // 完整管线（鉴权后中间件已跑过；渠道继承+重试）

        res := interceptor.Finish()                    // 本轮已透传(重写后)事件；错误/终止原因；捕获的虚拟调用
        if res.Fatal {                                 // 首轮未开始流前出错：拦截器已原样放行错误响应
            return
        }
        if res.StreamError {                           // 流开始后出错：已合成 SSE error 事件
            return
        }
        loop.aggregate(res.Usage)

        if len(res.VirtualCalls) == 0 || res.HasClientToolUse || iter == plan.MaxIterations {
            finalTurn := iter == plan.MaxIterations && len(res.VirtualCalls) > 0 && !res.HasClientToolUse
            _ = finalTurn                              // 见下：达上限时的拒绝轮
            emitFinal(loop, res)                       // 合成 message_delta(聚合 usage, stop_reason) + message_stop
            postToolBilling(c, loop)                   // §5.7
            return
        }

        // 执行本轮虚拟工具（ticker 并发写 ping 保活，间隔 5s）
        results := executeCalls(c, res.VirtualCalls, plan, loop)   // 配额/max_uses 检查在内部，拒绝也产出文本结果
        emitServerToolBlocks(realWriter, loop, res.VirtualCalls, results)  // server_tool_use + *_tool_result，全局索引

        body = appendIteration(body, res, results)     // assistant(text?+tool_use) + user(tool_result×n)
        loop.Offset += res.BlockCount
    }
}
```

**达迭代上限的收尾**：当 `iter == MaxIterations` 且本轮仍有虚拟调用且无客户端工具调用——不再执行，直接把拒绝文本作为 tool_result 回喂**并再跑一轮**（该轮为绝对最后一轮，循环自然退出），保证模型以 `end_turn` 文字收尾。实现上将拒绝轮并入循环计数（`MaxIterations` 含拒绝轮）。

**迭代 body 构造规则**：
```jsonc
// 追加到 messages 末尾：
[
  {"role":"assistant","content":[ /*<text块>?, {"type":"tool_use","id":"...","name":"web_search","input":{...}} */]},
  {"role":"user","content":[{"type":"tool_result","tool_use_id":"...","content":"<面向模型的紧凑文本>"}]}
]
```
- text 块保留（模型上一轮说过的话）；thinking 块不加（转换层会跳过，省 token）；
- 面向模型的 tool_result 为**紧凑文本渲染**（非 JSON 块原样），限额见 §5.6 各执行器；
- 追加的块已在 sanitized 形态（tool_use/tool_result 本就在白名单），`injectExcelChineseLanguage` 已在 adapter 阶段执行过，不会重复注入。

### 5.5 SSE 流改写器（`service/exceltools/interceptor.go`，新）

包装 `gin.ResponseWriter`（全方法委托，只拦截 `Write`/`WriteString`），逐行解析 `event:`/`data:` 并按状态机处理：

| 上游事件 | 处理 |
|---|---|
| `message_start` | 全部吞掉（第 1 轮前编排器已发合成版，见下）；捕获 message id/model |
| `ping` | 透传 |
| `content_block_start` | `tool_use` 且 `name ∈ 虚拟集` → 进入**抑制模式**（吞掉，开始记录该调用）；否则 `index += offset` 透传 |
| `content_block_delta` | 抑制模式 → 累积 `input_json_delta` 片段；否则 `index += offset` 透传 |
| `content_block_stop` | 抑制模式 → 完成该调用记录（id/name/input）；否则透传 |
| `message_delta` | 吞掉；记录 stop_reason，usage 计入聚合 |
| `message_stop` | 吞掉；标记本轮结束 |
| `error` | 透传并标记 StreamError（循环终止） |

**首轮 message_start 由编排器立即合成下发**（不等上游，客户端即刻进入接收态）：
```jsonc
event: message_start
data: {"type":"message_start","message":{"id":"msg_<loopid>","type":"message","role":"assistant","model":"<route后模型>","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}
```

**工具执行期间保活**：编排器在调用执行器时启动 2s ticker 向真实 writer 写 `event: ping`（协议原生事件；客户端与中间层均不会断）。code_execution 最长 30s 期间约发 15 个 ping，开销可忽略（见 §12 问题 2 的降级结论）。

**错误路径**：
- 首轮流开始前 Relay 失败（写 JSON 错误）：拦截器未发过任何 SSE 事件 → **原样放行**（客户端看到与现状一致的非 200 错误响应）；
- 流开始后（迭代 ≥1 已透传事件）Relay 失败写 JSON：拦截器将其转译为 `event: error` + `data: {"type":"error","error":{...}}` 并终止循环——客户端收到流内错误，与上游流中断的现有体验一致。

**最终收尾**：编排器合成 `message_delta`（聚合 usage：input/output 为各轮之和，与计费口径一致；stop_reason 取最后一轮真实值）+ `message_stop`。

### 5.6 执行器框架 `service/exceltools/`（新包）

```go
type ExecResult struct {
    Block      any    // 客户端渲染：*_tool_result 块的 content 字段（形状适配层，§4.5）
    ModelText  string // 模型喂给：紧凑文本，执行器负责限量
    Err        *ToolError
}

type Executor interface {
    Name() string
    TypePrefixes() []string                       // "web_search_" / "web_fetch_" / "code_execution_"
    Definition() ToolDefinition                   // S1 的 name/description/input_schema 单一事实来源
    Available() bool                              // 配置齐全与否（决定 strip 降级）
    Execute(ctx context.Context, input json.RawMessage, s *Session) *ExecResult
}
```

`Session`：userId、request_id、单请求计数器（max_uses）、Redis 日配额句柄、结构化 logger。

#### 5.6.1 web_search

- 供应商抽象 `SearchProvider`，配置选择。2026-09-07 选型结论（同日二次修订）：**智谱 web-search-pro 单家接入**（用户否决博查；智谱账号随 GLM 渠道现成，接入摩擦与商务动作均为零）；`bing` 已从清单划掉（Bing Search API 于 2025-08-11 退役）；`searxng` 不采用（需额外常驻服务，与零运维部署冲突）。若联调用 20 条金融 query 实测质量不达标，加第二家（火山引擎 AI 搜索 / Tavily 等）只需实现一个 `SearchProvider`（半天）+ 配置 `fallback_provider`；
- **按请求模型选供应商**（2026-09-07 补）：循环层读取路由后模型名（`RouteExcelModel` 结果，ExcelRequestAdapter 已写回 body），按 `provider_by_model_prefix` 前缀匹配解析供应商，无命中回退全局 `provider`。当前用户可选 GLM / DeepSeek 两模型，但 DeepSeek 平台无搜索 API——两家目前都解析到智谱，行为一致；机制为扩家预留（如将来接火山引擎搜索，配 `"deepseek-": "volc"` 即分流）；
- **GLM 原生联网搜索透传：二期评估，M1 不做**——智谱 API 支持把 web_search 作为原生工具由智谱侧执行计费，GLM 请求理论上可省网关执行；但需改渠道转换层（触碰 R2）且形成双路径行为差异，收益（省一层执行）不抵复杂度；
- **运行时自动 fallback**（机制保留，当前 `fallback_provider` 留空）：每次搜索先走解析出的供应商（主），命中失败条件（网络错误/超时/5xx/429/余额不足）才用备重试一次；query 参数错误、鉴权失败不触发（避免掩盖配置故障）。单次请求只走一家（不会双计费）；不做熔断状态机；日志记录实际命中厂商 `[excel-tools] ... provider=zhipu`；
- 结果：≤10 条 `{title, url, snippet≤300字, date?}`；
- `ModelText` 渲染：编号列表 `1. title — url\nsnippet`，整体 ≤4KB；
- `Block`（web_search_tool_result.content）：`[{type:"web_search_result", title, url, snippet}]`。

#### 5.6.2 web_fetch `service/exceltools/safefetch`（子包，SSRF 安全抓取器）

`SafeFetch(ctx, rawURL, opts) → (finalURL string, markdown string, err error)`：

1. **协议白名单**：仅 `http`/`https`（显式拒绝 file/ftp/gopher 等）；
2. **DNS pinning 防 rebinding**：自定义 `net.Dialer.Control`——解析 → 校验全部 IP → 拨号锁定已校验 IP（校验与连接之间无二次解析窗口）；
3. **IP 黑名单**（IPv4+IPv6）：`127/8`、`10/8`、`172.16/12`、`192.168/16`、`169.254/16`（含云元数据 `169.254.169.254`）、`0.0.0.0/8`、`100.64/10`、`::1`、`fc00::/7`、`fe80::/10`、IPv4-mapped `::ffff:0:0/96`；
4. **重定向**：禁用自动跟随（`CheckRedirect` 返回 `ErrUseLastResponse`），手动跟随 ≤3 跳，**每跳全量重走 1-3**；
5. **限额**：总超时 15s（可配）；`io.LimitReader` 5MB；自定义 UA；
6. **正文抽取**：readability 算法（引入 `go-readability` 类库）→ markdown 输出，≤80k 字符，超出截断并在 `ModelText` 头部标注 `[truncated]`；
7. `ModelText`：`<title>\n<final_url>\n\n<markdown>`；`Block`（web_fetch_tool_result.content）：`{type:"web_fetch_result", title, url, content:[{type:"text",text:markdown}]}`。

#### 5.6.3 code_execution `service/exceltools/sandbox`（子包）

```go
type Runner interface {
    Run(ctx context.Context, code string, files []FileSpec) (*RunResult, error)
}
// RunResult: {Stdout, Stderr string(各≤64KB截断), ExitCode int, DurationMs int64, TimedOut bool}
```

- **NsjailRunner**（2026-09-07 依部署约束修订：宿主机**不装 Docker**）：网关 `exec.Command` 调用 nsjail 静态二进制（单文件、无守护进程、无安装动作，位于网关数据目录），生命周期 = fork → 等待/墙钟超时强杀 → 收集 stdout/stderr → 进程退出（无清理步骤，进程即沙箱）。备选实现：Go 原生命名空间（`exec.Cmd` SysProcAttr Cloneflags），零外部文件但多 1-2 天开发且细节易踩坑，默认不选；
- 隔离参数（对齐 S2.3 红线的 nsjail 等价项）：`--chroot <rootfs>` + `/proc` 只读、`--user 65534 --group 65534`、`--time_limit 30`（`timed_out:true`）、`--rlimit_as 1024`、`--rlimit_cpu 30`、`--rlimit_nproc 64`、根文件系统 `--bindmount_ro` 只读、空网络命名空间（禁出网）、seccomp 默认策略起步（二期收紧）；
- 代码经 stdin 注入，工作目录为 chroot 内 `/tmp`（tmpfs 挂载 256MB，可写，根文件系统其余只读）；
- rootfs 分发：CI/dev 机器上用 Docker **构建一次**（`deploy/excel-sandbox/Dockerfile`：`python:3.11-slim` + pandas/numpy/openpyxl/pdfplumber/python-docx/lxml，**库清单与 description 承诺严格一致**），导出目录树打包为 `excel-sandbox-rootfs.tar.xz`（约 80-150MB）随发布物分发，宿主机解压到网关数据目录即用——生产宿主机全程不接触 Docker；
- `files` 参数 M1 仅定义不实现（S3 联动时挂载只读 `/files/input`）；
- 并发：per-user 1（Redis 锁 `excel_tools:lock:exec:{uid}` TTL=超时+5s）；全局信号量（配置，默认 4）；
- Runner 接口预留 E2B 等后端（若走 SaaS 沙箱需先过数据合规评审——用户表格数据出境问题，见 §12 风险 8）；
- `ModelText`：exit_code=0 → stdout（stderr 非空时附后）；非 0/超时 → 错误说明 + 截断的 stderr（模型可据此修代码重试）；`Block`（code_execution_tool_result.content）：`{type:"code_execution_output", text: stdout, stderr}`。

#### 5.6.4 调试端点（可选，flag 控制，默认关）

`POST /excel/api/tools/{web_search|web_fetch|code_execution}`：TokenAuth + 用户级限流，直接调执行器，用于联调与压测。挂 `/excel` 组（R1）。

### 5.7 计费与配额

- 迭代计费：见 §4.4，零新代码；
- 工具按次：`postToolBilling` → `service/exceltools.PostExcelToolConsumeQuota`（新）：价格取 `operation_setting.GetToolPrice`（与 `tool_billing.go` 同源），quota 换算复用 `ComputeToolCallQuota` 逻辑，独立账单行；默认 0；
- 日配额：Redis `INCR excel_tools:q:{tool}:{uid}:{yyyymmdd}` + `EXPIRE 86400`，超限 → 该次调用返回文本结果 `"今日额度已用完"`（不执行、不报 HTTP 错误，模型可向用户解释）；
- max_uses：Session 内计数，超出 → `"本回合调用次数已达上限"`。

### 5.8 日志

- 循环日志：`[excel-tools] request_id=%s iter=%d tool=%s user=%d dur_ms=%d status=%s`（status: ok / quota_denied / max_uses / exec_error / timeout）；入参只记 query/url，code 截断 200 字符；
- `excel_adapter.go:138` body 日志截断 2KB（§5.2 顺带修复）；
- 未知工具 type 前缀告警（§5.2，vendor 升级哨兵）。

---

## 6. SSE 协议细节：端到端示例

问"今天上证指数收盘多少"，客户端一次请求，完整下行流：

```jsonc
event: message_start
data: {"type":"message_start","message":{"id":"msg_ab12","role":"assistant","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}

// ── 迭代 1 透传（索引重写：+0）──
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}
event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"用户要实时数据，我需要搜索…"}}
event: content_block_stop
data: {"type":"content_block_stop","index":0}
// 模型的 web_search tool_use 块被拦截器抑制（不下发）
// （工具执行期间：event: ping × N）

// ── 网关合成（全局索引 1、2）──
event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srvtoolu_01","name":"web_search","input":{}}}
event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"上证指数 收盘 2026-09-07\"}"}}
event: content_block_stop
data: {"type":"content_block_stop","index":1}
event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_01","content":[{"type":"web_search_result","title":"…","url":"…","snippet":"…"}]}}
event: content_block_stop
data: {"type":"content_block_stop","index":2}

// ── 迭代 2 透传（索引重写：+3）──
event: content_block_start
data: {"type":"content_block_start","index":3,"content_block":{"type":"text","text":""}}
event: content_block_delta
data: {"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"上证指数今日收于…"}}
event: content_block_stop
data: {"type":"content_block_stop","index":3}

// ── 网关收尾 ──
event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":3120,"output_tokens":486}}
event: message_stop
data: {"type":"message_stop"}
```

客户端下一轮请求回传的 `server_tool_use`/`web_search_tool_result` 历史块由 §5.3 转换，模型保有搜索上下文。

---

## 7. 安全设计

| 面 | 措施 |
|---|---|
| web_fetch SSRF | §5.6.2 全清单：协议白名单、DNS pinning 防 rebinding、IPv4/IPv6 内网段+云元数据黑名单、重定向逐跳校验、体积/时长限额 |
| 沙箱逃逸 | R4 三红线 + nsjail 命名空间/seccomp/non-root/rlimit（宿主机无 Docker，见 §5.6.3）；上量后可升级 gVisor/Firecracker 或独立沙箱节点 |
| 沙箱资源滥用 | per-user 并发 1 + 日配额 + 全局信号量 + 30s 强杀 |
| 工具滥用（搜索/抓取） | per-user 日配额（Redis），拒绝为文本结果不打断对话 |
| 鉴权 | 循环与调试端点全部位于 TokenAuth 之后；无新增无鉴权面 |
| 信息泄露 | 日志脱敏（body 截断、code 截断、不记搜索结果全文） |
| 成本失控 | 每轮 PreConsume 天然限额 + MaxIterations + max_uses + 工具配额 |

---

## 8. 上线计划与回滚（2026-09-07 已定：一把梭 + 跳过白名单）

单次部署，发布物四样一起替换：网关二进制 + `nsjail` 静态二进制 + `excel-sandbox-rootfs.tar.xz` + 配置（`mode=strip` 起步）。

| 时点 | 动作 | 说明 |
|---|---|---|
| T0 部署 | 全量代码 + 沙箱文件，`mode=strip`、`sandbox.enabled=true` | strip 基线观察：UnknownToolError 循环应立即消失；基本对话/表格工具零回归 |
| T0+数小时 | 自测 + 真机过回归矩阵 → `mode=full` **全体用户直接生效（跳过白名单）** | 决策依据：用户量小、回退成本 = 改一行配置 |
| 之后 | 监控工具调用率 / 配额拒绝率 / 迭代分布 / Sentry | 异常 → `mode=strip` 秒级回退 |

- 回滚 = 改配置（`full → strip/off`），无需回滚代码；极端情况换回旧二进制（发布物是文件替换，天然可逆）；`off` 保留字节级现状兼容；
- 真机验证合成块渲染（§4.5，唯一未经原版环境验证的协议面）提前到 T0 自测阶段完成；
- 上线前检查两项：① `sysctl kernel.unprivileged_userns_clone` 开启（主流发行版默认开）；② **反代读超时 ≥ 60s**（2026-09-07 确认链路存在反代）——如 nginx `proxy_read_timeout` 等，过短会在工具执行静默期掐断连接（ping 保活 2s 可对冲，超时配置仍建议 ≥60s）；
- **前端配合项暂缓（2026-09-07 决策）**：搜索入口（规则4）不恢复。若 vendor 按钮同时控制 `web_search` 工具声明（T0 用 vConsole 看 tools 数组验证），则联网能力保持休眠直到前端恢复入口；`code_execution` / `web_fetch` 无独立按钮、随 caps 注入，不受影响。网关侧实现不变；
- T0 真机回归矩阵覆盖：**微软 Excel + WPS 双宿主**（存量客户端版本 + 最新构建）。
- 沙箱部署物说明：宿主机**不装 Docker**，nsjail + rootfs 均为数据目录下的文件（§5.6.3）。

回归矩阵（每阶段必跑）：

| 项 | 断言 |
|---|---|
| 基本对话（DeepSeek/GLM、思考两态） | 与现状逐字节一致（零成本路径无回归） |
| 表格工具全链路 | 不受影响 |
| conductor | 不受影响（不触碰 caps 相关前端补丁链） |
| strip 模式联网问句 | 模型不调用工具、明确说明无法联网（不再道歉循环） |
| full 模式搜索问句 | 状态行"搜索了网页"、回答引用结果、多轮历史保持 |
| full 模式计算问句（标准差/回归/IRR） | 状态行"运行了 N 个脚本"、数字正确 |
| 死循环代码（`while True: pass`） | 30s 超时、`timed_out` 喂回、模型体面收尾 |
| 迭代上限 | 构造连续调用场景 ≤5 轮收尾、流完整闭合 |
| 额度中途耗尽 | SSE error 优雅终止，无悬挂连接 |

---

## 9. 测试计划

- **单元**：前缀匹配（含未知后缀）、schema 注入、历史块转换、SSE 改写器（golden 文件：单轮/多轮/thinking 混合/客户端工具终止/上限拒绝轮/流中错误）、SSRF 校验表驱动（内网段/元数据/rebinding/重定向/协议）、沙箱 runner（超时杀、输出截断、禁网验证）、配额计数；
- **集成**：httptest 起假上游（脚本化 OpenAI SSE：先 tool_call 后答案），驱动 `/excel/v1/messages` 全链路（真实 token + SQLite），覆盖迭代构造与账单多行断言；
- **真机**：§8 回归矩阵 + 前端 vConsole 抓包核对合成块形状；
- **性能**：无虚拟工具请求的零成本路径基线对比（拦截器不激活时耗时无统计差异）。

---

## 10. 工作量

| 项 | 估算 |
|---|---|
| S1 + strip + 历史转换 + 配置骨架 + 日志脱敏 | 1 天 |
| 循环编排器 + SSE 改写器（含 golden 测试） | 3 天 |
| safefetch + web_fetch 执行器 | 1.5-2 天 |
| 搜索供应商抽象 + web_search 执行器（智谱单家） | 1 天 |
| 沙箱 rootfs 打包 + NsjailRunner + 红线测试 | 2-2.5 天 |
| 工具计费 + Redis 配额 | 1 天 |
| 集成测试 + 真机回归 | 2 天 |
| **合计** | **11-12.5 人日**（部署物 = 二进制 + nsjail + rootfs tarball，均为文件放置，无安装动作） |

---

## 11. 风险登记

| # | 风险 | 概率 | 缓解 |
|---|---|---|---|
| 1 | 客户端不渲染合成块（GrowthBook 门控 / 形状不符） | 中 | 发布 2 前真机验证；形状隔离在 RenderResult 单点；最坏降级：无状态行但功能可用；极端回滚 strip |
| 2 | 拦截器与 gin.ResponseWriter 委托缺陷（Flush/WriteString 边角） | 低 | 全接口委托 + 零成本路径不激活；集成测试覆盖流式 flush |
| 3 | 沙箱冷启动延迟（nsjail fork + import pandas ≈1-2s/次） | 确定 | ping 保活防断连；产品侧预告；二期 rootfs 常驻 + 进程池 |
| 4 | DeepSeek 不主动调用工具 | 中 | schema description 已做行为引导；监控调用率，必要时在 excel 中文注入段附加工具使用提示 |
| 5 | 每轮独立账单多行，ops/用户困惑 | 低 | 上线通知注明口径；二期聚合 |
| 6 | vendor 升级改变工具 type 前缀 | 低 | 前缀匹配 + 未知前缀告警哨兵 |
| 7 | 客户端 SSE 空闲超时过短（工具执行期间断连） | 低 | ping 保活；真机实测阈值 |
| 8 | E2B/SaaS 沙箱数据出境合规 | — | M1 只走自建 Docker；SaaS 后端接入前过合规评审 |
| 9 | 迭代内上游失败（渠道半路挂） | 中 | 渠道级重试自动生效（Relay 内置）；耗尽后 SSE error 优雅终止 |

---

## 12. 开放问题（需前端/产品/运维输入）

1. ~~工具块形状验证~~ **已定（2026-09-07）：跳过官方 key 抓包**，按 Anthropic 公开文档形状实现，T0 真机 vConsole 校正（§4.5 适配层隔离，最坏降级为无状态行、功能不受影响）；
2. 客户端 SSE 空闲超时阈值实测值——**已降级为验证项**：ping 间隔直接定为 2s（30s 工具执行约 15 个 ping、几百字节开销，可忽略），不实测也不构成风险；联调时顺手验证流不被掐即可。同时确认加载项到网关之间是否存在反代及其 `proxy_read_timeout`；
3. ~~沙箱部署形态~~ **已确认（2026-09-07）：裸脚本部署、宿主机不装 Docker → nsjail + 预构建 rootfs**（§5.6.3）；遗留检查项：宿主机 `kernel.unprivileged_userns_clone` 是否开启；
4. ~~搜索供应商选型~~ **已定（2026-09-07，同日二次修订）：智谱 web-search-pro 单家**（博查被否、Bing 已退役、SearXNG 不采用，理由见 §5.6.1）；商务侧零动作，确认智谱账户余额即可；
5. 工具计费价格策略（默认 0 上线，何时开启、定价多少）。

---

## 附：`/excel/*` 路由约束映射（R1 全景）

| 能力 | 端点 | 里程碑 |
|---|---|---|
| 工具循环 | 无新增（`/excel/v1/messages` 内闭环） | M1 |
| 调试执行器 | `POST /excel/api/tools/{name}`（flag 控制） | M1 |
| 文件服务 | `/excel/v1/files*` | M2 |
| 语音 ASR | `WS /excel/api/voice_stream` | M3 |
| MCP 代理 | `/excel/v1/mcp_servers*`、`/excel/v1/mcp/*` | M4 |
| 技能云同步 | `/excel/v1/skills*` | M5 |

全部位于 `router/excel-gateway-router.go` 的 `/excel` 组内，共享 `RouteTag`/`TokenAuth`/限流中间件；不触碰任何其他路由组。

---

## 13. 实现状态与偏差记录（2026-09-07 全量落地）

**状态：M1 全量代码完成**，`go build ./...`、`go vet`（新增包零告警）、单元/桩循环测试全绿。
落地文件：`setting/excel_tools/`（config/definitions/availability）、`middleware/excel_adapter.go`（改造）、
`service/exceltools/`（interceptor/emitter/loop/types/quota/billing/provider/zhipu_search/websearch/
webfetch/safefetch/sandbox/codeexec/debug）、`controller/excel_tool_loop.go`、`controller/excel_tool_debug.go`、
`router/excel-gateway-router.go`（挂载）、`deploy/excel-sandbox/`（Dockerfile + build-rootfs.sh + README）。
测试：`service/exceltools/stream_test.go`（拦截器 golden + 桩循环 4 场景）、`safefetch_test.go`、
`middleware/excel_adapter_tools_test.go`、`setting/excel_tools/definitions_test.go`。

### 与设计的偏差（均为实现时决策，机制等价或约束更严）

| # | 偏差 | 理由 |
|---|---|---|
| 1 | `FullUserIds` 用户白名单未实现 | 上线决策已跳过白名单（§8 一把梭），全局 mode 足够；加回只需在 BuildLoopPlan 前置一个集合判断 |
| 2 | 工具价格未新增 Prices 配置组，复用 `operation_setting.GetToolPriceForModel` | 与 relay 侧 tool_billing 同源同表，避免两套价格 |
| 3 | web_fetch 正文抽取降级为 TextContent（标题+URL+纯文本），未做 markdown DOM 渲染 | go-readability 的 DOM 节点类型跨版本不稳；模型消费纯文本无损失；isolated in `safefetch.extract` |
| 4 | 首轮 message_start 采用"惰性改写"（转发真实事件但清零 usage）而非"抢先合成" | 流开始前的 relay 失败可原样透传（Fatal 路径字节不变），错误语义不被污染 |
| 5 | 迭代内裸 JSON 错误体（无 SSE 框架、无结尾换行）由 `interceptor.finish()` 兜底转译为 SSE error 事件 | 否则该错误会被行缓冲吞掉，客户端误收"empty response" |
| 6 | 沙箱并发：全局信号量（首用时按 config 定容，改配需重启）+ 同用户串行（in-flight 计数） | 少用户场景够用；避免运行时重建 channel 的竞态 |
| 7 | `excel_tools` 包含两个测试钩子 `ApplyForTest`/`ResetForTest` | 单测需要旁路 DB 配置体系；运行时代码不调用 |
| 8 | 实现中修复：off 模式对**已识别**虚拟工具曾整条丢弃（违背字节级回滚承诺），已改为空 schema 放行（与未知 server tool 同路径） | 测试 TestSanitizeToolsOffModeLegacyPassthrough 锁死该行为 |
| 9 | 实现中修复：resultContent 对"无 Err 无 Block"结果（上限/配额拒绝、空搜索）曾渲染为"工具执行失败"，已改为透出 ModelText | 拒绝文案必须客户端可见（§4.4 终止语义） |

### 遗留到 T0 的事项

- vConsole 抓包核对合成块形状（§12-1）；
- 宿主机 `kernel.unprivileged_userns_clone` 检查（部署 README P1）；
- rootfs 在任一 Docker 机器上执行 `deploy/excel-sandbox/build-rootfs.sh`（用户侧动作）；
- T0 验证清单：`docs/excel-server-tools-t0-verification.md`。

### 前端逆向情报落地（2026-09-07 二批，前端同学提供 vendor bundle 分析）

vendor 侧确认的事实与对应处理：

| vendor 事实 | 对本实现的影响 |
|---|---|
| 声明形态：`{type:"web_search_20250305",name,max_uses}` / `{type:"code_execution_20250825"}` / `{type:"web_fetch_20250910",max_content_tokens:100000}`，均无 `input_schema` | 与 `sanitizeExcelTools` 检测条件（顶层 type + 无 schema + 前缀匹配）完全吻合；三个真实版本号已进测试表 |
| `web_search max_uses: 1` | 已捕获并生效：Session 内同请求第二次调用被拒（"次数已达上限"），与 Anthropic per-request 语义一致 |
| `web_fetch max_content_tokens: 100000` | **新增捕获**：VirtualToolInfo.MaxContentTokens → Session → webfetch 预算（tokens×4 字符，与 80KB 硬顶取小）；默认值下硬顶更严，vendor 调小时自动收紧 |
| 客户端注册表（gln/X5）无任何 server tool 本地执行器 | strip 降级路线安全：模型不调用、客户端也不会本地执行；确认"服务端执行"假设（方案 A 成立的根基） |
| 状态行由分类表 ONn 按块内 `name` 渲染（web_search→web、web_fetch→fetch、bash→jit） | emitter 合成 `server_tool_use` 块必须用裸名（web_search 等）——现有实现即如此；风险 §11-1（客户端不渲染）降级为低 |
| `*_tool_result` 块的渲染形状仍未见 vendor 证据 | 维持 T0 vConsole 核对项；最坏情况无状态行，功能不受影响 |

### 前端逆向情报落地（2026-09-07 三批：结果块渲染三层结构，已实证）

| vendor 事实（已实证） | 对本实现的影响 |
|---|---|
| 渲染第一层"状态行"来自 ONn 分类表（按 name 查表），不依赖结果块内容 | 与二批结论一致；裸名 server_tool_use 即可 |
| 渲染第二层"来源列表"：SSE 处理器（vendor:291193）对 `web_search_tool_result` 的 `content[]` 中 `type==="web_search_result"` 条目**只取 title、url** 写入 store（setWebSearchResults，全 bundle 唯一写入点）；`encrypted_index` 不参与 | **方案 A 直接受益**：网关合成块走同一条 SSE 通路，现有形状 `{type:"web_search_result",title,url,snippet}` 满足点亮条件（snippet 为多余字段，无害）。降级文本结果不点亮面板——符合预期 |
| 渲染第三层"正文角标"走 text 块 `citations:[{type:"web_search_result_location",encrypted_index,…}]`，为 Anthropic 服务端特有机制 | 自建执行器无法提供；**已知限制**：无角标引用，仅影响展示，无功能影响（已接受，不追） |
| 配对规则：`tool_result.tool_use_id` 必须等于 `server_tool_use.id`；孤儿结果块被 Compactor 转成文本占位（vendor:285658），不崩 | emitter.go 构造保证：同一 `call.ID` 同时写入 server_tool_use.id（L27）与 tool_use_id（L51）；历史回放两侧块均原样携带，配对恒成立 |
| 方案 B（前端 client tool_use/tool_result 执行器）不经过上述 SSE 处理器 → 来源列表默认不亮；补救为前端一行 `setWebSearchResults(results.map(u=>({title,url})))` | 方案 B 前端侧自理；**方案 A 无需任何补救**（合成块即 SSE 通路） |
