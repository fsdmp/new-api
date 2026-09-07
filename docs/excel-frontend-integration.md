# Excel 插件（cc-excel-web）× 网关接入文档

> 面向前端（cc-excel-web 插件仓库）的完整接入参考：认证、模型、中继、文件（M2）、
> MCP 连接器（M4）、技能云同步（M5）、构建期配置与联调方法。
> 网关 = new-api 分支（生产 https://newapi.offiai.cn，下文以 `<GW>` 代指）。
> 更新：2026-09-07，M4/M5 网关侧已上线。

---

## 0. 总览

```
插件（WPS Excel 加载项）
  │  所有请求带 Authorization: Bearer sk-xxx + New-Api-User: <uid>
  ▼
网关 newapi.offiai.cn
  ├─ /excel/v1/messages        模型中继（anthropic messages 兼容）
  ├─ /excel/v1/models          模型列表（免认证）
  ├─ /excel/v1/files/*         文件上传/列表/下载/删除（M2）
  ├─ /excel/v1/mcp_servers     MCP 连接器注册表（M4）
  ├─ /excel/v1/mcp/{server_id} MCP JSON-RPC 同域代理（M4）
  ├─ /mcp-registry/v0/servers  连接器图标注册表 stub（M4，域根路径）
  ├─ /api/oauth/organizations/ccexcel/skills/*   技能云同步（M5，域根路径）
  ├─ /excel/api/*              版本检查/埋点/eval 等 stub（免认证）
  └─ /excel/api/tools/:tool    server tools 调试端点（M1，TokenAuth + 开关）
```

**路径前缀规则（url-rewrite.js 运行时覆盖层）**：构建期所有 anthropic 域名已被
build-plugin.mjs 替换为 `<GW>`；运行时覆盖层对 fetch/XHR URL 做前缀改写：

| 插件原始路径 | 实际请求 | 说明 |
|---|---|---|
| `<GW>/v1/*` | `<GW>/excel/v1/*` | 中继/文件/MCP 加 `/excel` 前缀 |
| `<GW>/api/oauth/organizations/...` | **原样不动** | isDirectBackendApi 白名单，M5 技能走这里 |
| `<GW>/mcp-registry/v0/servers` | **原样不动**（无匹配段） | 图标注册表在域根 |

---

## 1. 认证

### 1.1 网关令牌（所有业务请求）

插件 profile 形如：

```js
claude.inference.profile = {
  kind: "gateway",
  url: "https://newapi.offiai.cn",
  token: "sk-xxxxxx",        // 网关令牌，客户端持 sk- 前缀完整串
}
```

每个请求带两个头：

```
Authorization: Bearer sk-xxxxxx
New-Api-User: <用户数字 id>
```

注意：**服务端 tokens 表存的是裸 key（无 sk- 前缀）**，客户端必须拼 `sk-` 后发送；
`New-Api-User` 必须与令牌归属用户一致，否则 TokenAuth 拒绝。

### 1.2 临时令牌（未认证首次进入）

```
POST /excel/tmp-key
→ { token: "sk-...", user_id: <n>, expire_days: n }
```

由网关 `excel_tmp_key.*` 选项控制（公共账户 + 过期天数 + 额度）。

### 1.3 错误信封（网关统一 anthropic 风格）

```json
{ "type": "error", "error": { "type": "not_found", "message": "..." } }
```

认证失败时网关可能返回 new-api 风格
`{"error":{"message":"Invalid token ...","type":"new_api_error"}}`——前端按
HTTP 401 统一处理即可。

---

## 2. 模型列表与中继

### 2.1 模型列表（免认证）

```
GET /excel/models              # 或 GET /excel/v1/models
→ [{ id, display_name, ... }]
```

来源是管理面板「Excel 模型列表」（`excel_model_list.models` 选项）。

### 2.2 消息中继

```
POST /excel/v1/messages
Content-Type: application/json
（标准 anthropic messages 请求体）
```

- 适配层自动做：工具声明消毒（server tools 按模式 strip/full）、中文指令注入、
  **视觉路由**（带图消息发给纯文本模型时自动改路由到第一个 `vision!=false` 模型）；
- 响应为标准 anthropic messages SSE / JSON；
- 超时/重试遵循 anthropic SDK 默认。

---

## 3. 文件上传（M2，Files API 兼容子集）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/excel/v1/files` | multipart，字段 `file`；可带 `purpose` |
| GET | `/excel/v1/files` | 列表（本人） |
| GET | `/excel/v1/files/:id` | 下载 |
| DELETE | `/excel/v1/files/:id` | 删除（行+磁盘+沙箱三向清理） |

约束（`excel_tools.files` 选项，留空走默认）：单文件 20MB、每用户 100 个 /
500MB、TTL 30 天、扩展名白名单（pdf/csv/xlsx/xls/docx/txt/md/json/png/jpg/
jpeg/webp/gif）、单次 run 最多引用 5 个 file_id。

沙箱内只读桥接路径：`/files/input/<file_id>/<原始文件名>`（硬链接，代码执行工具
的提示词里注入）。消息里 `document` 块引用 file_id 时网关自动校验归属并注入内容。

---

## 4. MCP 连接器（M4）

### 4.1 注册表

```
GET /excel/v1/mcp_servers?limit=100&page=
anthropic-beta: mcp-servers-2025-12-04
Authorization / New-Api-User 必带
```

响应：

```json
{
  "data": [
    {
      "id": "deepwiki",
      "display_name": "DeepWiki",
      "url": "https://mcp.deepwiki.com/mcp",
      "icon_url": "",
      "tools": [
        {
          "name": "read_wiki_structure",
          "description": "...",
          "input_schema": { ... },
          "effective_max_permission": "blocked?"   // 仅被封禁工具携带
        }
      ]
    }
  ],
  "next_page": null
}
```

语义：
- 上游不可达的 server **仍在列表**，但 `tools: []`（UI 应允许展示 + 重试）；
- `tool_policies` 里配为 `blocked` 的工具**不删除**，带
  `effective_max_permission: "blocked"` 下发——前端按"管理员已禁用"处理
  （不展示或置灰）；
- 工具在**插件端执行**：名称前缀 `mcp__<sanitized>__<tool>`，`sanitized` =
  display_name 小写、非 `[a-z0-9]` 字符替换为 `_`、裁剪到 64 字符
  （与网关 `excel_mcp.SanitizeName` 同规则）；执行结果作为普通 tool_result
  回传 `/excel/v1/messages`，**不经过 MCP 计费**。

### 4.2 JSON-RPC 代理（同域透传）

```
POST /excel/v1/mcp/{server_id}
Authorization / New-Api-User 必带
Content-Type: application/json
Accept: application/json, text/event-stream
（可选回显请求头 mcp-session-id）
```

- 请求体原样透传上游；支持方法白名单：`initialize`、`ping`、`tools/list`、
  `tools/call`、`notifications/*`（其余 → 400 invalid_request）；
- 响应可为**裸 JSON 或 SSE**（`data:` 行流），前端两种都要能解析；
- 上游若返回 `mcp-session-id` 响应头，后续请求原样回传即可（无状态 1:1 透传，
  网关不落库）；
- 时序约定：`initialize` → 回显 session → `notifications/initialized` →
  `tools/list` / `tools/call`；
- 错误语义：上游 401 → 前端标记该 server `needs_auth`（弹头配置/联系管理员）；
  **404 → 丢弃本地 session 重新 initialize**；网关代理失败 → 502
  `upstream_error`；未知 server_id → 404；
- 超时：`tools/call` 用服务器配置（默认 300s，上限 600s），其余方法 30s。

### 4.3 图标注册表（stub）

```
GET /mcp-registry/v0/servers        # 注意：域根路径，不带 /excel
→ { "servers": [] }
```

v1 恒返空数组，前端静默跳过 icon 回填即可（`icon_url` 留空显示首字母）。

---

## 5. 技能云同步（M5）

组织标识固定为 **`ccexcel`**（构建常量，见 §7）。四个端点全部在
**域根** `/api/oauth/organizations/ccexcel/skills` 下，认证同 §1.1。

### 5.1 列表

```
GET /api/oauth/organizations/ccexcel/skills/list-skills
→ [   // 裸数组（{data:[...]} / {skills:[...]} 两种包裹也兼容）
  {
    "id": "skill_b571bbac18953ec3e3e898b1",   // skill_ 前缀 → 触发客户端 zip 下载
    "name": "demo-skill",
    "description": "M5 acceptance demo skill",
    "enabled": true,
    "source": "remote",
    "creator_type": "user",
    "is_shared": false,
    "partition_by": "account",
    "size_bytes": 378,
    "created_at": 1788779444,
    "updated_at": 1788779444
  }
]
```

- **可编辑判定**（对应插件 O3()）：`creator_type === "user" && !is_shared &&
  partition_by === "account"` —— 即仅本人上传的技能可在 UI 编辑/删除；
  `partition_by === "organization"` 的为管理员下发，全员只读；
- 仅返回 `enabled=true` 的技能（本人 account 的 + 全部 organization 的）。

### 5.2 读取指令（read_skill 数据源）

```
GET .../skills/:skill_id/instructions
→ { "instructions": "---\nname: demo-skill\n...SKILL.md 全文...\n" }
```

注意插件侧 5 秒超时，响应即 SKILL.md 全文（≤2MB），不要二次请求。

### 5.3 下载 zip

```
GET .../skills/:skill_id/download
→ 200 application/zip
  Content-Disposition: attachment; filename="demo-skill.skill"
```

zip 内必须有 `SKILL.md`（可在子目录）；客户端解压后读 SKILL.md 的
frontmatter（name/description）+ 正文。

### 5.4 上传（用户侧）

```
POST .../skills/upload-skill?overwrite=true&check_skill_name=<name>
Content-Type: multipart/form-data
  file = <name>.skill（或 .zip，服务端只认内容）
```

| 场景 | 响应 |
|---|---|
| 成功 | 200 + §5.1 的技能对象 |
| 同名已存在（无 overwrite） | **409** `name_conflict`（提示可携带 overwrite 覆盖**自己的**技能；他人占用同名也是 409——org+name 组织级唯一） |
| 覆盖 | `?overwrite=true` → **原行替换**（id 不变，下载缓存自然失效） |
| 超限 | 403 `quota_exceeded`（每人 50 个）；413 `request_too_large`（zip > 10MB） |
| 非法 zip | 400 `invalid_request`（无 SKILL.md / 非法路径 / 名字不合 `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`） |

zip 硬限制：≤10MB、≤500 条目、解压总量 ≤30MB、必须含 SKILL.md。

---

## 6. 其余网关端点（stub / 运维）

| 端点 | 说明 |
|---|---|
| `GET /excel/healthz` | 探活 |
| `GET /excel/shortcuts.json` | 快捷指令 mock |
| `GET/POST /excel/v1/metrics`、`/excel/v2/traces`、`POST /excel/api/analytics`、`POST /excel/api/eval/:sdkKey` | 遥测/评测 mock（恒 200） |
| `GET /excel/api/version` | 版本 mock |
| `GET /excel/api/check-version` | 最低版本检查（`excel_version_check.minimum_versions` 选项），低于版本返回更新提示 |
| `POST /excel/api/tools/:tool` | M1 server tools 调试端点（`excel_tools.debug_endpoints` 开启时可用） |

---

## 7. 构建期配置（cc-excel-web 仓库）

`.env` / `.env.example`：

```bash
# 网关地址（所有 anthropic 域名替换目标）
VITE_GATEWAY_URL=https://newapi.offiai.cn
# WPS 加载项根 URL / staging 域名（原有项，不变）
VITE_ADDIN_URL=...
VITE_STAGING_HOST=...
# M5 技能组织标识（网关 excel_skills.org_id）
VITE_INTERNAL_ORG_UUID=ccexcel
```

**重要**：vendor bundle 里 `import.meta.env` 已被原构建整体替换为空对象
`const Fie = {}`，`.env` 的 VITE_* 不会自动生效。`VITE_INTERNAL_ORG_UUID` 由
`scripts/build-plugin.mjs` 显式 split/join 注入（默认 `ccexcel`，无 .env 也能构建）：

```js
// build-plugin.mjs（已实现）
const ORG_UUID = ENV.VITE_INTERNAL_ORG_UUID || 'ccexcel'
result = result
  .split('const Fie = {},')
  .join(`const Fie = { VITE_INTERNAL_ORG_UUID: "${ORG_UUID}" },`)
```

改动 .env 后执行 `node scripts/build-plugin.mjs`（或 `npm run build`）重新构建，
产物在 `public/m-addin/`。

---

## 8. 管理面板配置项（运维视角，classic 主题「Excel 设置」页）

| 选项行 | 作用 |
|---|---|
| `excel_mcp.enabled` | MCP 连接器总开关 |
| `excel_mcp.servers` | JSON 数组：`[{"id","display_name","url","headers"?,"enabled","timeout_sec","tool_policies"?}]`；id 限 `^[a-z0-9_-]{1,64}$`；headers 透传上游鉴权；blocked 工具打禁用标记 |
| `excel_mcp.tools_cache_sec` / `excel_mcp.max_concurrent` | 注册表缓存（默认 300s）/ 代理并发（默认 16，上限 64） |
| `excel_tools.files` | M2 文件上传（见 §3） |

选项保存后约 **60 秒**生效，无需重启。

---

## 9. 联调方法（curl 等价物）

```bash
GW=http://127.0.0.1:3000            # 生产机上；外部用 https://newapi.offiai.cn
AUTH="Authorization: Bearer sk-xxx"; N="New-Api-User: 1"

# MCP 注册表
curl -s "$GW/excel/v1/mcp_servers?limit=100" -H "$AUTH" -H "$N"

# MCP 代理（initialize）
curl -s -X POST "$GW/excel/v1/mcp/deepwiki" -H "$AUTH" -H "$N" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"cc-excel","version":"1.0"}}}'

# 技能列表 / 上传
curl -s "$GW/api/oauth/organizations/ccexcel/skills/list-skills" -H "$AUTH" -H "$N"
curl -s -X POST "$GW/api/oauth/organizations/ccexcel/skills/upload-skill" \
  -H "$AUTH" -H "$N" -F "file=@demo.skill"
```

## 10. 验收清单（真机）

- [ ] 插件构建产物含注入的 org UUID（grep `VITE_INTERNAL_ORG_UUID` 于 public/m-addin）；
- [ ] Skills 面板出现管理员下发的 organization 技能；read_skill 实调返回指令；
- [ ] 用户上传同名技能 409 → overwrite=true 成功且 id 不变；
- [ ] MCP：+ 菜单出现连接器、tools/list 可见、tools/call 实调成功、
      blocked 工具被拒绝、断网 server 显示可用性错误但列表不消失；
- [ ] 中继/文件/视觉回归：带图消息路由到 vision 模型、文件上传下载正常。
