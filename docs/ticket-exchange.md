# 换票（Ticket Exchange）前端接入流程

> 适用场景：插件（浏览器扩展）需要跨域登录 new-api，但无法共享主站 cookie session。通过服务端临时票中转，最终让插件拿到 **access_token**（请求头 `Authorization` 凭证，对应 `users.access_token` 字段）。
>
> 换的是 access_token，**不是** cookie session。

---

## 0. 角色与职责

| 角色 | 是什么 | 职责 | 持有的凭证 |
|---|---|---|---|
| **插件端** | 浏览器扩展，无法共享主站 cookie | 发起换票、轮询取 token、后续调业务 API | 最终拿到 `access_token` |
| **浏览器端「另外页面」** | 在外部浏览器打开的独立页面（不在 new-api 仓库，由接入方实现） | 读 URL 上的 ticket、引导用户登录、调 bind 把 token 关联到 ticket | 登录后持有 `access_token`，通过 `Authorization` header 调 bind |
| **服务端** | new-api 后端 | 提供 3 个换票接口 | — |

## 1. 三方时序图

```
插件端                       服务端                    浏览器端(另外页面)
  │                           │                           │
  │ ① POST /api/ticket        │                           │
  │ ─────────────────────────►│                           │
  │   ◄── { ticket }          │                           │
  │                           │                           │
  │ ② 打开外部浏览器到「另外页面」?ticket=xxx ──────────────►│
  │                           │                           │
  │ ③ 启动轮询循环              │                           │
  │   GET /api/ticket/:t      │                           │
  │ ─────────────────────────►│ status=pending            │
  │   ◄── {status:"pending"}  │                           │
  │        (每 2s 重复)         │                           │
  │                           │  ④ 用户输入账密登录          │
  │                           │ ◄── POST /api/user/login   │
  │                           │ ──── access_token+uid ────►│
  │                           │                           │
  │                           │  ⑤ POST /api/user/ticket/bind
  │                           │     Authorization:<token>  │
  │                           │     New-Api-User:<uid>     │
  │                           │     {ticket}               │
  │                           │ ◄──────────────────────── │
  │                           │ ──── {status:"bound"} ────►│ ⑥ 清理 URL 的 ticket
  │                           │                           │
  │ ③' GET /api/ticket/:t     │                           │
  │ ─────────────────────────►│ status=bound              │
  │   ◄── access_token+user   │                           │
  │                           │                           │
  │ ⑦ 停止轮询，持久化 token，关闭外部浏览器                    │
```

**「外部浏览器已登录」补充场景**：「另外页面」打开时本地已持有有效 access_token，则跳过第④步直接进第⑤步 bind。这是支持「多插件 / 多域名」的关键——同一用户可 bind 多个不同 ticket，每个 ticket 独立。

---

## 2. 插件端流程

### 步骤
1. **创建临时票**：`POST /api/ticket` → 拿到 `ticket`
2. **打开外部浏览器**：把 `ticket` 拼到「另外页面」URL 上打开（`https://domain/your-bind-page?ticket=xxx`）。具体打开方式由插件决定：`chrome.tabs.create` / `window.open` / native messaging 等
3. **立即启动轮询**：每 2 秒调 `GET /api/ticket/:ticket`
4. **拿到 bound 结果后**：停止轮询，持久化 `access_token` + `user_id`，关闭外部浏览器（可选）

### 伪代码
```js
const API_BASE = 'https://your-new-api-domain'

// ① 创建临时票
async function createTicket() {
  const res = await fetch(`${API_BASE}/api/ticket`, {
    method: 'POST',
    // CriticalRateLimit 已在服务端挂载；创建接口无需 TurnstileCheck
  })
  const json = await res.json()
  if (!json.success) throw new Error(json.message)
  return json.data.ticket
}

// ③ 轮询单次
async function pollTicket(ticket) {
  const res = await fetch(`${API_BASE}/api/ticket/${ticket}`)
  return res.json() // { success, data: { status, access_token, user_id, ... } }
}

// 轮询循环（带超时）
async function waitForToken(ticket, { intervalMs = 2000, timeoutMs = 11 * 60 * 1000 } = {}) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const json = await pollTicket(ticket)
    if (!json.success) throw new Error(json.message)      // ticket 过期/不存在
    if (json.data.status === 'bound') return json.data    // ✅ 拿到 token
    // status === 'pending' → 继续等
    await new Promise(r => setTimeout(r, intervalMs))
  }
  throw new Error('polling timeout')
}

// 主流程
async function loginViaExternalBrowser() {
  const ticket = await createTicket()
  // ② 打开外部浏览器到「另外页面」（URL 自行实现）
  openExternalBrowser(`${YOUR_BIND_PAGE_URL}?ticket=${encodeURIComponent(ticket)}`)
  // ③ ④ ⑤ ⑥ ⑦
  const tokenData = await waitForToken(ticket)
  // 持久化（chrome.storage / localStorage）
  await saveCredentials({
    accessToken: tokenData.access_token,
    userId: tokenData.user_id,
    username: tokenData.username,
  })
  return tokenData
}

// ⑦ 后续调业务 API：带两个 header
async function callBusinessApi(path) {
  const { accessToken, userId } = await loadCredentials()
  return fetch(`${API_BASE}${path}`, {
    headers: {
      'Authorization': accessToken,
      'New-Api-User': String(userId),
    },
  })
}
```

### 轮询参数建议
| 参数 | 建议值 | 说明 |
|---|---|---|
| 轮询间隔 | 2000 ms | 平衡实时性与服务端压力（接口挂了 `CriticalRateLimit`） |
| 超时 | 11 分钟 | 略大于 pending 的 10 分钟 TTL |
| 退出条件 | `status=bound` / 错误 / 超时 | 任意一个触发即停 |

---

## 3. 浏览器端「另外页面」流程

**核心职责**：从 URL 取 `ticket` → 确保已登录 → 调 `bind` → 清理 URL。

### 步骤
1. **读 ticket**：`new URLSearchParams(location.search).get('ticket')`
2. **判断登录态**：从本地存储读 `access_token` + `user_id`（接入方登录后存的）
3. **分支**：
   - **已登录** → 直接跳到步骤 5 调 bind
   - **未登录** → 展示登录表单（账密 / OAuth / Passkey 任选），登录成功拿到 `access_token` + `user_id` 后再走步骤 5
4. （未登录时的登录）调现有 `POST /api/user/login`，响应体 `data` 里已包含 `access_token` 和 `id`
5. **调 bind**：`POST /api/user/ticket/bind`，带 `Authorization` + `New-Api-User` header，body 带 `{ticket}`
6. **清理 URL**：bind 成功后用 `history.replaceState` 去掉 `?ticket=`，避免 ticket 泄漏到浏览器历史 / Referer

### 伪代码
```js
const API_BASE = 'https://your-new-api-domain'

// ④ 未登录时的登录（复用现有接口，返回里含 access_token）
async function login(username, password) {
  const res = await fetch(`${API_BASE}/api/user/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  const json = await res.json()
  if (!json.success) throw new Error(json.message)
  // data: { id, username, display_name, role, status, group, access_token, require_2fa? }
  return json.data
}

// ⑤ 关联 ticket
async function bindTicket(ticket, { accessToken, userId }) {
  const res = await fetch(`${API_BASE}/api/user/ticket/bind`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': accessToken,          // ← access_token（非 cookie）
      'New-Api-User': String(userId),        // ← 必须带，服务端会校验与 token 匹配
    },
    body: JSON.stringify({ ticket }),
  })
  const json = await res.json()
  if (!json.success) throw new Error(json.message)
  return json.data   // { status: "bound" }
}

// 页面入口
async function onBindPageMount() {
  // ① 读 ticket
  const ticket = new URLSearchParams(location.search).get('ticket')
  if (!ticket) return                       // 无 ticket 则是普通访问，走正常逻辑

  // ② 判断登录态（接入方自管的存储 key）
  let creds = loadStoredCredentials()

  // ③④ 未登录则先登录
  if (!creds) {
    creds = await showLoginFormAndSubmit()  // 内部调 login()，返回 {access_token, id}
    saveStoredCredentials(creds)
  }

  // ⑤ 调 bind
  try {
    await bindTicket(ticket, { accessToken: creds.access_token, userId: creds.id })
    // ⑥ 清理 URL 中的 ticket
    const url = new URL(location.href)
    url.searchParams.delete('ticket')
    history.replaceState({}, '', url.toString())
    showSuccess()
  } catch (e) {
    // ticket 过期/已绑定 → 提示用户回插件重新发起
    showError(e.message)
  }
}
```

### 「已登录则直接 bind」是关键场景
插件可能多次换票，用户第一次登录后，「另外页面」打开时本地已有凭证，应**跳过登录、直接 bind**——这正是支持「多插件 / 多域名」的方式（同一用户 bind 多个不同 ticket，每个 ticket 独立）。

---

## 4. 接口速查表

| 接口 | 方法 | 路径 | 鉴权 | 调用方 |
|---|---|---|---|---|
| 创建临时票 | POST | `/api/ticket` | 无 | 插件端 |
| 轮询换票 | GET | `/api/ticket/:ticket` | 无 | 插件端 |
| 关联临时票 | POST | `/api/user/ticket/bind` | UserAuth（access_token 或 cookie） | 浏览器端「另外页面」 |

### 请求 / 响应示例

**POST /api/ticket**
```json
// 响应
{ "success": true, "message": "", "data": { "ticket": "aB3xK9..." } }
```

**GET /api/ticket/:ticket**
```json
// pending
{ "success": true, "data": { "status": "pending", "created_at": 1730000000 } }

// bound（插件停轮询，取走这些字段）
{ "success": true, "data": {
    "status": "bound",
    "access_token": "sk-xxxxxxxx",
    "user_id": 1,
    "username": "root",
    "display_name": "Admin",
    "role": 100,
    "user_status": 1,
    "group": "default"
}}

// 过期/不存在（HTTP 200，项目标准错误）
{ "success": false, "message": "ticket not found or expired" }
```

**POST /api/user/ticket/bind**
```
Header: Authorization: <access_token>
        New-Api-User: <user_id>
Body:   { "ticket": "aB3xK9..." }
```
```json
// 成功
{ "success": true, "data": { "status": "bound" } }
// 已绑定
{ "success": false, "message": "ticket already bound" }
// 过期/不存在
{ "success": false, "message": "ticket not found or expired" }
```

### TTL 与状态机
| 状态 | TTL | 含义 |
|---|---|---|
| `pending` | 10 分钟（创建起算） | 等待外部浏览器 bind，超时自动失效 |
| `bound` | 5 分钟（bind 起算） | 已关联，等插件轮询取走，期间可幂等重复 GET |

同一 ticket 只能 bind 一次（`pending → bound` 单向），重复 bind 报 `ticket already bound`；同一用户可 bind 多个不同 ticket。

---

## 5. 错误处理与边界

| 场景 | 表现 | 前端处理 |
|---|---|---|
| 用户在 10 分钟内未完成登录 | 插件轮询拿到 `ticket not found or expired` | 提示超时，引导重新点登录 |
| 用户在外部浏览器关了页面没 bind | 插件轮询到 TTL 过期 | 同上 |
| ticket 被 bind 两次（理论上不会，除非刷新重试） | `ticket already bound` | 浏览器端忽略即可，插件端正常拿 token |
| 用户账密错误 | `/api/user/login` 返回 `success:false` | 「另外页面」显示错误，让用户重输 |
| 用户禁用态 | bind 返回 `user is disabled` | 提示账号被禁 |
| `New-Api-User` header 与 token 不匹配 | bind 返回 401 `user id mismatch` | 检查 header 是否传对 |
| 网络抖动 | fetch 抛异常 | 轮询继续；登录 / bind 给 2~3 次重试 |

---

## 6. 安全注意

1. **ticket 是一次性中转凭证**：不要写进日志、不要让 ticket 长期出现在前端控制台；浏览器端 bind 成功后**立即 `history.replaceState` 清掉 URL 的 ticket**
2. **access_token 等同密码**：插件端建议用 `chrome.storage.local`（而非 `localStorage`）存储，避免 XSS 拿走
3. **必须 HTTPS**：ticket 和 access_token 都是明文传输，HTTP 下可被中间人截获
4. **轮询别太密**：接口有 `CriticalRateLimit`，2 秒间隔足够，更密可能被限流
5. **关闭外部浏览器**：插件拿到 token 后，如外部浏览器是插件打开的可控窗口，可主动关闭；非可控窗口则不强求

---

## 7. 完整接入 checklist

**插件端**
- [ ] `POST /api/ticket` 创建临时票
- [ ] 打开外部浏览器到「另外页面」`?ticket=xxx`
- [ ] 轮询 `GET /api/ticket/:ticket`，2s 间隔，11min 超时
- [ ] 收到 `bound` 后持久化 `access_token` + `user_id`
- [ ] 后续业务请求带 `Authorization` + `New-Api-User` header

**浏览器端「另外页面」**
- [ ] 从 URL 读 `ticket`，无 ticket 走普通逻辑
- [ ] 检测本地登录态（`access_token` + `user_id`）
- [ ] 未登录 → 展示登录表单 → 调 `POST /api/user/login` → 存凭证
- [ ] 已登录 → 直接进 bind
- [ ] `POST /api/user/ticket/bind`（带 `Authorization` + `New-Api-User` + `{ticket}`）
- [ ] 成功后 `history.replaceState` 清理 URL 上的 `ticket`
- [ ] 错误分支：ticket 过期 / 已绑定 → 提示用户回插件重试

---

## 8. 后端实现索引

| 文件 | 作用 |
|---|---|
| `service/ticket_service.go` | 临时票存取（Redis + 内存兜底）、TTL 与状态机 |
| `controller/ticket.go` | 3 个 handler：`CreateTicket` / `GetTicket` / `BindTicket` |
| `router/api-router.go` | 路由注册（`POST /api/ticket`、`GET /api/ticket/:ticket`、`POST /api/user/ticket/bind`） |
