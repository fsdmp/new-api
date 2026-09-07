# Excel 网关 Server Tools 接入文档（前端向）

> 对象：Excel 加载项前端。本文覆盖网关侧新增的三项 server tool（web_search / web_fetch /
> code_execution）对客户端可见的全部行为变化。
>
> 结论先行：**请求侧零改动**；响应侧会出现 4 种新的 SSE 内容块；建议补 1 条状态行分类规则。
> 其余（鉴权、端点、SSE 基础协议）与现状完全一致。

---

## 1. 背景

- 加载项按 vendor 原生形态声明 server tool（`web_search_20250305` 等带版本号的 type，无
  `input_schema`），此前网关默认 **strip 模式**：这三个工具被静默移除，模型回答时会声明"没有联网搜索"。
- 现在网关支持 **full 模式**：三项工具由**网关在服务端执行**（搜索走智谱、抓取走网关安全 fetch、
  代码跑在 nsjail 沙箱），客户端**不需要内置任何执行器**——与逆向结论一致（客户端注册表
  gln()/X5 中本就没有 server tool 的本地执行器）。
- 模式由网关后台配置切换（`excel_tools.mode`：`off` / `strip` / `full`），对前端透明：
  - `strip`（当前默认）：响应里**不会出现**任何下述新块，行为与线上现状一致；
  - `full`：出现新块，本文档生效；
  - 前端按 `full` 模式开发即可，不需要感知切换时机。

## 2. 请求侧：零改动

继续原样发送 vendor 声明，网关负责识别与转换：

```jsonc
// POST /excel/v1/messages  （端点、鉴权、其余字段均不变）
{
  "model": "glm-4.6",
  "stream": true,
  "tools": [
    { "type": "web_search_20250305",    "name": "web_search", "max_uses": 1 },
    { "type": "web_fetch_20250910",     "name": "web_fetch",  "max_content_tokens": 100000 },
    { "type": "code_execution_20250825","name": "code_execution" }
  ],
  "messages": [ ... ]
}
```

字段说明：

| 字段 | 网关行为 |
|---|---|
| `max_uses` | 生效：整个请求内该工具的执行次数上限（默认按工具配置，web_search 声明 1 即 1 次） |
| `max_content_tokens` | 生效：web_fetch 回灌文本预算（≈4 字符/token，且与 80KB 硬顶取小） |
| 未知的 server tool type（如未来 vendor 新增 `memory_20270101`） | 网关不识别 → 按普通工具处理，不会注入也不会执行 |
| 工具开关 | 网关侧配置决定（搜索需配 key、沙箱需部署），未就绪的工具会被剥离并给模型降级提示，请求不会失败 |

**注意**：工具循环只在 `"stream": true` 时生效（加载项恒为流式，无影响；用 curl 调试时记得带）。

## 3. 响应侧：新增的 SSE 内容块

full 模式下，模型调用 server tool 时，原本的 `tool_use` 块被拦截（客户端不会看到模型原始的
`tool_use` + `tool_result` 往返），网关执行后以 **成对的合成块** 回放：

### 3.1 `server_tool_use` —— 工具调用（回放）

```
event: content_block_start
data: {"index":3,"content_block":{"type":"server_tool_use","id":"srvtoolu_xyz","name":"web_search","input":{}}}

event: content_block_delta
data: {"index":3,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"今日金价\"}"}}

event: content_block_stop
data: {"index":3}
```

- `name` 是**裸名**（`web_search` / `web_fetch` / `code_execution`），不带版本号——与状态行
  分类表的键一致。
- `partial_json` **一次性携带完整入参 JSON**（不逐字流式），解析时直接 JSON.parse 即可。

### 3.2 `web_search_tool_result` —— 搜索结果

```
event: content_block_start
data: {"index":4,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_xyz","content":[
  {"type":"web_search_result","title":"...","url":"https://...","snippet":"..."},
  {"type":"web_search_result","title":"...","url":"https://...","snippet":"..."}
]}}

event: content_block_stop
data: {"index":4}
```

结果块的内容**整体放在 `content_block_start` 里**，`content_block_stop` 前不会有 delta。

> **已实证（vendor 三批逆向）**：来源列表面板的 SSE 处理器（vendor:291193）对
> `content[]` 中 `type === "web_search_result"` 的条目**只读 `title`、`url` 两个键**
> （`setWebSearchResults` 唯一写入点）。网关合成块的形状满足点亮条件——**方案 A 下
> 来源面板应当自动亮，前端无需任何补救**（`snippet` 是多余字段，无害；降级文本结果
> 不点亮面板，符合预期）。`encrypted_index` 不参与来源列表，不需要提供。

### 3.3 `web_fetch_tool_result` —— 网页抓取

```jsonc
{"index":5,"content_block":{"type":"web_fetch_tool_result","tool_use_id":"srvtoolu_abc","content":[
  {"type":"web_fetch_result","title":"页面标题","url":"https://最终地址（跟随重定向后）",
   "content":[{"type":"text","text":"标题\nURL\n\n正文提取文本…"}]}
]}}
```

### 3.4 `code_execution_tool_result` —— 沙箱执行

```jsonc
{"index":6,"content_block":{"type":"code_execution_tool_result","tool_use_id":"srvtoolu_def","content":[
  {"type":"code_execution_result","content":[{"type":"text","text":"stdout 输出…\nstderr: ..."}]}
]}}
```

### 3.5 降级/失败时的结果块形状

工具被拒（日配额用尽 / max_uses 用尽 / 执行失败 / 超时）时**不会报错中断**，结果块的
`content` 退化为纯文本，模型会基于降级提示继续回答：

```jsonc
{"type":"web_search_tool_result","tool_use_id":"...","content":[
  {"type":"text","text":"今日该工具使用额度已用完，请明天再试或基于已有信息回答。"}
]}
```

超时的 code_execution 文本为 `代码执行超时（超过时间限制被强制终止）…`，非零退出码带
`代码执行失败（退出码 N）` 前缀。**渲染上可以按普通文本处理，不区分也完全不影响功能。**

## 4. 完整事件序列（一轮带工具的对话）

模型先输出一段文字，再调 web_search，网关执行后续写第二轮回答。客户端看到的完整流：

```
event: message_start        ← 只出现一次；usage 一律为 0（见 §6.3）
data: {"type":"message_start","message":{...,"usage":{"input_tokens":0,"output_tokens":0}}}

event: content_block_start  ← 模型文字块（index 0）
event: content_block_delta  ← text_delta 逐段
event: content_block_stop

event: content_block_start  ← 合成 server_tool_use（index 1）
event: content_block_delta  ← input_json_delta（完整 JSON）
event: content_block_stop

event: ping                 ← 工具执行期间每 ~2s 一个心跳（对抗反代空闲超时）
data: {"type":"ping"}

event: content_block_start  ← 合成 web_search_tool_result（index 2）
event: content_block_stop

event: content_block_start  ← 第二轮模型文字块（index 3，编号全局连续）
event: content_block_delta
event: content_block_stop

event: message_delta        ← 网关合成；stop_reason + 多轮累计 usage（真实值在这里）
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},
       "usage":{"input_tokens":1234,"output_tokens":567,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}

event: message_stop
data: {"type":"message_stop"}
```

要点：

1. **index 全局连续递增**：多轮迭代的块序号由网关统一重排，客户端按常规逻辑分配/回收即可。
2. **每轮迭代的 `message_start` / `message_stop` 被吞掉**，客户端只会看到一对消息起止帧。
3. **`stop_reason`**：正常 `end_turn`；达到迭代上限模型仍想调工具时强制 `end_turn`；
   响应里含**客户端工具**（如 sheet 操作）时为 `tool_use`——此时虚拟工具的结果也已回放完毕，
   客户端照常走自己的工具循环即可。
4. **`ping` 事件**新增于工具执行期间，客户端原有 ping 处理逻辑（忽略）即可覆盖。

## 5. 错误形状

上游/网关错误以 Anthropic 风格的 SSE error 事件出现（可能出现在流中任意位置，出现即终止，后面不再有 message_stop）：

```
event: error
data: {"type":"error","error":{"type":"api_error","message":"upstream error: ..."}}
```

与现有错误处理一致，无需新增逻辑。

## 6. 结果块渲染与前端待办（按已实证的三层结构）

前端同事实证的渲染分层（vendor bundle 分析）：

| 层 | 链路 | 方案 A（网关合成块）下的表现 |
|---|---|---|
| ① 状态行（"搜索了网页"） | ONn 分类表，按块内 `name` 查表，不依赖结果块内容 | ✓ 裸名 `server_tool_use` 即触发 |
| ② 来源列表（Search results） | SSE 处理器（vendor:291193）读 `web_search_tool_result.content[]` 中 `type==="web_search_result"` 条目的 **title + url** → `setWebSearchResults`（全 bundle 唯一写入点）→ 渲染来源面板 | ✓ 合成块走同一条 SSE 通路，**面板应自动点亮，前端零改动**；`encrypted_index` 不参与，不需要 |
| ③ 正文角标引用 | text 块 `citations:[{type:"web_search_result_location",encrypted_index,…}]` | ✗ Anthropic 服务端特有机制，自建执行器给不了；**已知限制**：无角标，仅影响展示，无功能影响（已接受） |

配对规则（已实证）：`tool_result.tool_use_id` 必须等于 `server_tool_use.id`；孤儿结果块会被
Compactor 转成文本占位（vendor:285658），不会崩。**网关侧由构造保证配对**（同一 id 同时写入
两个块），前端只要原样回放历史即恒成立。

### 6.1 补一条状态行分类（建议）

现有分类表按块内裸名映射状态行：`web_search` → web、`web_fetch` → fetch、`bash` → jit。
**`code_execution` 在表里没有条目** → 不会显示状态行，功能不受影响。建议补一条（图标/文案
自定，例如显示"正在运行代码…"）。

### 6.2 来源面板联调确认（唯一剩余核对项）

§3.2–3.4 的合成块形状按 Anthropic 文档形状实现，②的键位要求（title/url）已核对满足；
T0 联调时用 vConsole 确认面板确实点亮即可。若有出入，**网关端单点可调**
（`service/exceltools/emitter.go` 是唯一形状出口），前端不用兜底兼容两种形状。

（注：方案 B——前端 client `tool_use`/`tool_result` 执行器——不经过 ② 的 SSE 处理器，
来源面板默认不亮，补救为前端执行器返回后调一次
`rt.getState().setWebSearchResults(results.map(u => ({title: u.title, url: u.url})))`。
方案 A 无需此补救。）

### 6.3 历史回放（确认现有逻辑覆盖）

下一轮请求的 `messages` 里会出现上一轮的 `server_tool_use` + `*_tool_result` 块（与 vendor
原生行为一致）。前端把这两类块**原样存入会话历史、原样带回**即可，网关会负责把它们转换成
上游可理解的 `tool_use` / `tool_result`。不要在本地把 `*_tool_result` 的 content 改写或截断。

## 7. 已知边界（不影响加载项，列出来备查）

| 边界 | 说明 |
|---|---|
| `message_start` 的 usage 恒为 0 | 真实 token 用量以**最终 `message_delta`** 为准（多轮累计）。前端若有 token 计数，务必取终帧 |
| 非流式请求不进循环 | `"stream": false` 时网关不执行工具（加载项恒为流式，仅影响 curl 调试） |
| 计费按轮累计 | 工具循环每轮 Relay 正常计费 + 工具按次计费（默认 0），日志里一轮一行 |
| web_search 结果数 | 默认取前 5 条（模型可传 `max_results` ≤10 微调） |
| code_execution 环境 | Python 3.11 + pandas/numpy/openpyxl/pdfplumber/python-docx/lxml，30s 超时，禁网，无第三方安装 |

## 8. 联调自测工具

### 8.1 直连执行单个工具（不走模型）

后台开启 `excel_tools.debug_endpoints` 后可用（默认关；用加载项同款 token 鉴权）：

```bash
curl -s -X POST https://<网关>/excel/api/tools/web_search \
  -H "Authorization: Bearer sk-xxx" -H "Content-Type: application/json" \
  -d '{"query":"今日金价","model":"glm-4.6"}'

# 返回
{
  "tool": "web_search",
  "status": "ok",            // ok / timeout / nonzero_exit / exec_error / invalid_input ...
  "duration_ms": 812,
  "model_text": "搜索结果（query: 今日金价）：\n1. ...",
  "block": [ {"type":"web_search_result","title":"...","url":"...","snippet":"..."} ],
  "error": null               // 失败时 {"code":"exec_error","message":"..."}
}
```

`/excel/api/tools/web_fetch` 传 `{"url":"https://..."}`；`/excel/api/tools/code_execution`
传 `{"code":"print(1+1)"}`。返回的 `block` 与 SSE 流里的合成块同构，可用于先行开发渲染逻辑。

### 8.2 完整链路

正常走 `/excel/v1/messages`（`stream: true` + 带工具声明），vConsole 抓 SSE 核对 §4 序列。

### 8.3 网关连通性

`GET /excel/healthz`（已有，不变）。

## 9. 已知限制与发布回滚

**已知限制（已接受，不追）**：正文角标引用（text 块 `citations` / `encrypted_index` 机制）
为 Anthropic 服务端特有，网关执行器无法生成——表现为回答正文没有 [1][2] 角标；
来源列表面板（②）不受影响，正常点亮。

**发布与回滚**：
- full 模式上线是网关配置切换，不需要前端发版；回滚切回 `strip`（或 `off`）同样即时生效

- full 模式上线是**网关配置切换**，不需要前端发版；回滚切回 `strip`（或 `off`）同样即时生效。
- `strip` 模式下响应不含任何新块，旧版本前端在 full 模式下也能正常工作（新块会被当作未知块
  忽略/透传），**不存在前后端必须同步发版的约束**。
