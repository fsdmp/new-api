# Excel M4/M5 设计 — MCP 连接器（M4）与技能云同步（M5）

> 日期：2026-09-07 · 状态：**待评审**
> 依据：插件 bundle 逆向（cc-excel-web/addin/vendor/m-addin/assets/index-4Ixb1e-Z.js）
> + cc-excel-web 构建期 patch（scripts/build-plugin.mjs）+ overlay 运行时改写（url-rewrite.js / fetch-patch.js / cache-restore.js）

## 0. 一句话结论

插件对 M4/M5 的调用契约已完全确定，**网关侧可零前端改动实现 M4（MCP）**；
M5（技能）需要一个 cc-excel-web 构建期常量（组织 UUID）+ 网关四个端点。
两条链路都复用现有 TokenAuth（插件所有请求都带 sk- 令牌 + New-Api-User 头），共用中继管线零改动。

## 1. 插件端契约（逆向结论）

### 1.1 域名与路径改写链（现网已生效）

构建期（build-plugin.mjs）：
- `https://api.anthropic.com` → `VITE_GATEWAY_URL`（技能/MCP 注册表/profile 全在这域名下）
- `https://mcp-proxy.anthropic.com` → `VITE_GATEWAY_URL`（MCP 代理基址）

运行时（overlay url-rewrite.js）：
- `/v1/` → `/excel/v1/`（MCP 两个端点因此落在 /excel/v1/*，符合网关约定）
- 含 `/api/oauth/` 的 URL **不加前缀**（直连后端白名单）→ 技能端点落在 `/api/oauth/organizations/...`
  （与 new-api 自身 /api/oauth/github 等不冲突：gin 静态段不同）

鉴权：所有请求 `Authorization: Bearer <cc_excel_user_api_key>`（claude.inference.profile 的 token，
即 sk- 临时令牌）+ `New-Api-User: <uid>` → **现有 TokenAuth 直接可用，无需新鉴权**。

### 1.2 M4 — MCP 连接器

**注册表**：`GET /excel/v1/mcp_servers?limit=100&page=<cursor>`
- 头：`Authorization` / `anthropic-version: 2023-06-01` / `anthropic-beta: mcp-servers-2025-12-04`
- 期望响应：`{"data": [Server], "next_page": null}`（分页：data 页满则带 next_page 续拉，最多 10 页）
- `Server = {id, display_name, url, icon_url?, tools: [{name, description?, input_schema?, effective_max_permission?}]}`
- 插件启动 / 页面可见 / 模型调 `refresh_mcp_connectors` 时都会拉取；5s 超时；401/403 静默。

**代理**：`POST /excel/v1/mcp/{server_id}` — JSON-RPC over HTTP（streamable http）
- 方法序列：`initialize`（15s 超时）→ `notifications/initialized`（通知，无响应体）→ `tools/list` → `tools/call`（240s 超时）
- 会话：initialize 响应头 `mcp-session-id` 由插件保存并在后续请求原样带回 → **网关可无状态透传该头**（客户端会话 = 上游会话，1:1）
- 响应体：JSON 或 SSE 都可（插件双解析）；401 → 插件标记 needs_auth；404 → 重建会话
- 工具以 `type:"custom", name:"mcp__<server>__<tool>"` 声明进 /messages 的 tools 数组，
  **由插件客户端执行**（tools/call），结果以普通 tool_result 块回传 → **网关中继管线零改动、零计费改动**。
  已验证 excel adapter 对 type:"custom" 工具原样放行（name/description/input_schema 保留）。

**图标注册表**：`GET /mcp-registry/v0/servers?version=latest&limit=...&visibility=...`
- 仅用于补 icon_url；失败静默。返回 `{"servers": []}` 空实现即可。

### 1.3 M5 — 技能云同步

组织 UUID 来源：store 默认 `VITE_INTERNAL_ORG_UUID`（构建期烘焙）或 OAuth profile。
现网两者都没有 → **cc-excel-web 需加一个构建常量**（如 `VITE_INTERNAL_ORG_UUID=ccexcel`），否则技能面板拉取时带空 org 直接返回。

四个端点（基址 = GATEWAY_URL，路径不加 /excel 前缀）：
1. `GET /api/oauth/organizations/{org}/skills/list-skills`
   - 响应：数组或 `{data:[...]}`；条目：`{id, name, description, enabled, creator_type, is_shared, partition_by, is_public_provisioned?}`
   - `id` 以 `skill_` 开头才会触发客户端 zip 下载；`creator_type:"user" && !is_shared && partition_by:"account"` = 本人可编辑
2. `GET /api/oauth/organizations/{org}/skills/{skill_id}/download` → **zip 字节**（客户端解压，读 SKILL.md）
3. `GET /api/oauth/organizations/{org}/skills/{skill_id}/instructions` → `{"instructions": "<SKILL.md 全文>"}`（read_skill 工具用，5s 超时）
4. `POST /api/oauth/organizations/{org}/skills/upload-skill?overwrite=true&check_skill_name=<name>`
   - multipart `file`（文件名 `<name>.skill`，实为 zip），15s 超时

技能注入：启用技能的 instructions 进系统上下文（read_skill 读取）；UI「+ 菜单 → Skills」
列出 list-skills 结果；保存到本地 = 下载 zip 存 IndexedDB。

插件 bundle 里另有 Anthropic SDK 风格的 `/v1/skills?beta=true` 类封装，但 UI 实际走的是
上述 claude.ai 风格路径 —— 本设计只实现 claude.ai 风格四端点。

## 2. 网关侧设计

### 2.1 M4 数据模型与配置

无新表。MCP 服务器清单走 options 表（与 excel_tools 同模式，60s 生效）：

```
excel_mcp.servers = [
  {"id": "github", "display_name": "GitHub", "url": "https://mcp.github.com/mcp",
   "headers": {"Authorization": "Bearer xxx"}, "enabled": true, "timeout_sec": 300}
]
excel_mcp.enabled = true        // 总开关（false 时 mcp_servers 返回空 data）
excel_mcp.tools_cache_sec = 300 // 注册表响应里 tools 元数据的缓存时长
```

- `id` 即 server_id（URL 段，需 `^[a-z0-9_-]{1,64}$`）；`display_name` 会被插件转 sanitized 名（`mcp__<sanitized>__<tool>`）。
- `headers` 支持占位符吗？不支持 —— 插件的 `{{var}}` 模板是 bootstrap 配置路径才有的；注册表路径 headers 我们自己存明文（与 excel_tools.search.api_keys 同级别敏感度，仅管理员可读）。

### 2.2 M4 端点实现

**GET /excel/v1/mcp_servers**（TokenAuth）
1. 读配置，过滤 enabled；
2. 每个服务器取 tools 元数据：内存缓存（key=url+headers hash，TTL excel_mcp.tools_cache_sec），
   未命中则向 upstream 发一次 `initialize` + `tools/list`（3s 超时，失败→ tools:[]，不阻塞列表）；
3. 响应 `{data: [{id, display_name, url, icon_url:"", tools:[{name, description, input_schema}]}], next_page:null}`。
   `effective_max_permission` 由管理员在 `excel_mcp.servers[].tool_policies: {tool_name: "blocked"}` 配置时才输出。

**POST /excel/v1/mcp/{server_id}**（TokenAuth）
- 无状态透传代理：
  1. 按 server_id 查配置（不存在/禁用 → 404 anthropic 风格错误体）；
  2. 构造上游请求：URL=配置 url，method POST，body 原样，头 = 配置 headers + 透传
     `mcp-session-id`（客户端带回的）+ `Accept`（json/event-stream）+ `Content-Type`；
  3. 响应：状态码、body 流式透传（io.Copy，limitedBuffer 记日志）、透传 `mcp-session-id` 响应头；
  4. 超时：`initialize`/`tools/list` 30s、`tools/call` 用配置 timeout_sec（默认 300s）、其余 30s
     （按解析出的 JSON-RPC method 分档；解析失败一律 30s）。
- 并发：复用 exceltools 信号量模式（独立 mcpMaxConcurrent，默认 16）。
- 安全：仅 HTTPS 上游（生产强制）；响应体上限 32MB；SSRF 防护——url 仅来自管理员配置，不接受用户输入。

**GET /mcp-registry/v0/servers** → `{"servers": []}` 桩（免 404 噪音日志）。

### 2.3 M5 数据模型

新表 `excel_skills`（三库兼容，主键网关生成）：

| 列 | 类型 | 说明 |
|---|---|---|
| id | varchar(48) PK | `skill_` + 24hex（插件只认 skill_ 前缀才触发下载） |
| org_id | varchar(64) index | 组织（现阶段常量 ccexcel） |
| name | varchar(191) | SKILL.md frontmatter name（唯一键 org+name） |
| description | varchar(1024) | frontmatter description |
| instructions | mediumtext/TEXT | SKILL.md 全文（instructions 端点直读，免解压） |
| enabled | tinyint | 下线不删除 |
| creator_type | varchar(16) | "user" |
| is_shared | tinyint | false |
| partition_by | varchar(16) | "account"（=本人可编辑；"organization"=全员只读） |
| owner_user_id | int index | 上传者（partition_by=account 时用于隔离可见性） |
| zip_path | varchar(255) | 相对 data 目录：skills/<id>/skill.zip |
| size_bytes | bigint | |
| created_at / updated_at | bigint | |

存储布局：`<workdir>/skills/<skill_id>/skill.zip`（与 excel files 同目录约定，随网关工作目录）。

上传解析：服务端解 zip（archive/zip，防 Zip Slip：条目路径 `filepath.Clean` 后必须仍在目标内、
单文件 ≤10MB、总解压 ≤30MB）→ 找 `SKILL.md`（根目录或唯一一级目录下）→ 解析 frontmatter
（`---\nname: x\ndescription: y\n---`，无 frontmatter 则用 URL 参数 check_skill_name + 空 description）
→ instructions 存库，zip 落盘。

### 2.4 M5 端点实现（全部 TokenAuth）

1. **list-skills**：`owner_user_id = 当前用户 OR partition_by = 'organization'`，enabled 过滤，
   响应平铺数组（插件兼容数组 / {data} / {skills} 三种，用平铺数组最简）。
2. **download**：owner/组织校验 → `c.DataFromReader`（application/zip）。
3. **instructions**：owner/组织校验 → `{"instructions": <row.instructions>}`。
4. **upload-skill**：multipart ≤10MB → 解析（§2.3）→ 同名冲突：带 `overwrite=true` 覆盖，否则 409；
   配额：每用户 ≤50 个、每 org ≤200 个（可配）。

错误体沿用 Anthropic 风格 `{type:"error", error:{type, message}}`（与 /excel/v1/files 一致）。

### 2.5 管理面板（classic SettingsExcel 增量）

- `excel_mcp.servers` / `excel_mcp.enabled` TextArea + 开关（模式同 sandbox/files）；
- 技能管理 MVP：新「Excel 技能」卡片 —— 列表（表格：名称/描述/启用/可见范围/大小/时间）+
  上传（file input → POST）+ 启停 + 删除。走**管理员会话**端点 `/api/excel/skills*`
  （new-api 后台自身鉴权，非 sk- 令牌），挂在 router/api-router.go；
- 用户上传的技能（upload-skill）与技能面板数据同库，管理员可见全部。

### 2.6 cc-excel-web 侧改动（1 行 + 可选）

- `.env` + `.env.example` 加 `VITE_INTERNAL_ORG_UUID=ccexcel`（技能链路的前置条件）；
- 可选 patch：`claude.ai/customize/skills`、`claude.ai/customize/connectors` 外链改成官网地址
  （不改也能用，只是跳到 claude.ai）。

## 3. 安全清单

- 两条链路全部 TokenAuth；技能 owner/组织双重校验（越权 404，同 M2 不泄露存在性）；
- MCP 上游 URL/headers 仅管理员可配；代理仅透传方法白名单（tools/*、initialize、notifications/*），
  其余 method 400；上游必须 https；响应体/超时双限；
- zip 解压防 Zip Slip + 体积双重上限；frontmatter name 规范化（`^[a-zA-Z0-9_-]{1,64}$`）；
- 注册表 tools/list 探测失败不阻塞列表返回（可用性优先，UI 显示 0 工具可重试）；
- 不打印上游 headers/令牌到日志。

## 4. 与 M2 的一致性复用

- TokenAuth 前置模式、anthropic 错误体、`DataFromReader` 下载、options 表写配置、
  信号量并发、limitedBuffer 日志、网关生成 ID、三库兼容建表 —— 全部沿用 M2 已验证模式。
- 中继管线（/excel/v1/messages）零改动：MCP 工具是客户端执行，技能只进系统上下文文本。

## 5. 测试与验收（T0）

M4：
1. 配置一个公共 MCP（如 deepwiki）→ GET mcp_servers 返回带 tools 元数据；
2. 插件真实链路（真机）：+ 菜单出现连接器、工具列表可见、调用一次 tools/call 成功回结果；
3. 停用某 server → 列表消失、refresh 后模型不再引用；
4. blocked 策略 → 插件端报"已被管理员禁用"；
5. 无效上游（DNS 不存在）→ 列表仍返回（tools 空），代理 502。

M5：
1. 管理员上传 zip（含 SKILL.md）→ 列表可见 → 插件 Skills 面板出现 → 启用后模型能 read_skill；
2. 用户上传同名 → 409；overwrite=true → 覆盖；
3. 跨用户：A 传的 account 技能 B 不可见/下载 404；
4. 恶意 zip（../路径、超大）→ 400；
5. 纯文本对话零回归探针 2 条。

## 5.1 实施记录（2026-09-07，网关侧已上线）

发布：v20260907-1854（8a38b516）+ v20260907-1916（ac992711 修复覆盖上传）。

**服务端验收（生产实测，全部通过）：**
- M4 注册表：deepwiki 上游实探 3 工具回传；`tool_policies.blocked` 工具带
  `effective_max_permission:"blocked"` 标记；死上游出现在列表但 tools=[]；
  `/mcp-registry/v0/servers` → `{"servers":[]}`；
- M4 代理：initialize + tools/call（read_wiki_structure）经网关真实回数据
  （SSE 流式响应兼容）；死上游 → 502 upstream_error；未知 server → 404；
  方法白名单外（resources/list）→ invalid_request；未认证 → 401；
- M5 上传：合法 zip 200（skill_ 前缀 id）；同名 409 name_conflict；
  overwrite=true 原行覆盖（同 id，updated_at 刷新）；`../` 路径 400；
  缺 SKILL.md 400；
- M5 读取：list-skills / instructions / download 三端点正常，download
  字节级等于上传 zip；跨用户（account 分区）list 不可见、instructions/download
  404；跨 org → []；管理端点无会话 401；
- 回归：/excel/v1/messages 文本探针通过（thinking+text 块正常）；
- 管理面板：MCP 开关 + 服务器 JSON TextArea + 技能管理卡片（上传/可见性/
  启停/删除）已编译进 classic 主题；选项 `excel_mcp.*` 行级配置 60 秒拾取实测有效。

**与设计的偏差：**
1. blocked 策略=标记不下发删除：注册表保留该工具但带
   `effective_max_permission:"blocked"`，由插件端拒绝执行（与 claude.ai 行为一致），
   而非网关侧隐藏（§2.1 原文为"隐藏"，实做为标记）；
2. overwrite 覆盖语义改为**原行替换**（保持 id 与 zip 路径不变）——初版
   新建行撞 `(org_id, name)` 唯一索引返回 api_error，ac992711 修复；
3. org+name 唯一为**组织级**约束：不同用户不能各自建同名技能（B 上传 A
   已占用的名字会 409）——符合 claude.ai 语义，管理上需知悉；
4. 插件端（真机）验收待 cc-excel-web 重新构建部署后进行
   （build-plugin.mjs 已注入 `VITE_INTERNAL_ORG_UUID=ccexcel`，本地构建产物
   已验证）。

**生产配置终态：** `excel_mcp.enabled=true`，servers 仅 deepwiki
（无 blocked 策略）；验收用 token/技能行/测试 zip 已全部清理。

## 6. 工作量

| 项 | 估算 |
|---|---|
| M4：配置 + 注册表 + 无状态代理 + 桩 + 测试 | 1.5 人日 |
| M5：表 + 上传解析 + 四端点 + 测试 | 1.5 人日 |
| 管理面板（MCP TextArea + 技能管理卡片） | 1 人日 |
| 联调验收（含真机）+ 文档 | 0.5 人日 |
| **合计** | **4.5 人日** |

## 7. 评审点

1. **org UUID 常量**：VITE_INTERNAL_ORG_UUID=ccexcel 是否 OK（决定 M5 能否上线）；
2. **技能可见性默认值**：用户上传默认 partition_by=account（仅本人）+ 管理员可转 organization（全员共享）——符合预期吗；
3. **MCP 上游白名单**：是否需要限制仅内网/已审 URL，还是管理员配置即信任（当前设计=配置即信任）；
4. **图标**：v1 不做 icon_url（面板显示首字母），后续要再加 /mcp-registry 实现；
5. 上传配额 50/200、zip 10MB 是否合适。
