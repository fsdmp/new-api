# 微信公众号扫码登录（WeChat MP Scan Login）前端接入流程

> 适用场景：PC 网站希望**在页面内嵌二维码**完成微信登录，不跳转微信、不需要用户关注公众号回复验证码。基于公众号「带参数临时二维码 + SCAN 事件回调」实现。
>
> 本文面向**非默认前端**（即不是 `web/default`、`web/classic` 的第三方接入方），尤其是**无法使用 cookie session** 的客户端（浏览器扩展、桌面端、Electron、移动端 WebView、第三方 SPA 等）：登录后拿到 **access_token**（请求头 `Authorization` 凭证，对应 `users.access_token` 字段），用 `Authorization` + `New-Api-User` 两个 header 调后续业务 API。

---

## 0. 角色与职责

| 角色 | 是什么 | 职责 | 持有的凭证 |
|---|---|---|---|
| **客户端** | 接入方实现的前端（扩展/桌面端/SPA 等），**不能或不想**依赖 cookie | 调 qrcode 接口拿二维码、轮询 status、拿到响应中的 `access_token` + `id` 后持久化，后续业务请求带两个 header | 最终拿到 `access_token` + `user_id` |
| **微信客户端** | 用户手机里的微信 App | 扫码、确认登录 | — |
| **服务端** | new-api 后端 | 提供 3 个接口（生成二维码、轮询状态、微信回调）；在用户扫码后自动完成登录或注册 | — |
| **微信服务器** | mp.weixin.qq.com | 推送 SCAN 事件给服务端 | — |

## 1. 前置条件（管理员侧）

接入方需要先让 new-api 管理员完成以下配置，否则相关接口会返回错误：

1. **启用 Redis**：扫码登录依赖 Redis 暂存会话状态。`REDIS_CONN_STRING` 必须配置。
2. **开启功能开关**：管理后台 → 系统设置 → 认证设置 → 「微信公众号」标签页：
   - 勾选「启用微信公众号登录」（复用现有 `wechat_oauth.enabled`）
   - 勾选「启用扫码登录」（`scan_login_enabled`）
   - 填写 `AppId`、`AppSecret`
   - 填写 `Token`、`EncodingAESKey`（明文模式后者可不填）
3. **微信公众号后台配置**（mp.weixin.qq.com → 开发 → 基本配置 → 服务器配置）：
   - **URL**：`https://<your-new-api-domain>/api/mp/wechat/callback`
   - **Token**：与上一步一致
   - **EncodingAESKey**：与上一步一致（明文模式可不填）
   - **消息加解密方式**：明文模式
   - 启用后微信会发 GET 验签，服务端返回 `echostr` 即代表通过
4. **IP 白名单**：公众号后台 → 基本配置 → IP 白名单，加上 new-api 服务器的出网 IP（用于调 `cgi-bin/token` 拿 access_token）

### 如何探测是否已就绪

调 `GET /api/status`，关注以下字段：

```json
{
  "wechat_oauth": true,
  "wechat_oauth_appid": "wx...",
  "wechat_mp_login": true,
  "wechat_login_username_prefix": "wechat_",
  "wechat_oauth_username_prefix": "wechat_",
  "wechat_mp_username_prefix": "wechat_mp_"
}
```

- `wechat_mp_login=true` 表示管理员已开启扫码登录，可以走本流程
- `wechat_mp_username_prefix` 是本渠道新建用户的用户名前缀，用于客户端区分用户来源（仅做展示，不参与请求逻辑）

---

## 2. 时序图

```
客户端                       new-api 服务端               微信服务器 / 公众号后台
  │
  │ ① POST /api/oauth/wechat-mp/qrcode
  │    {aff_code?:""}
  │ ─────────────────────────►
  │                           │ ② 生成 scene_id（前缀 sl_ + 32 位随机）
  │                           │    Redis 写入 status=pending（TTL 180s）
  │                           │ ③ GET /cgi-bin/token 拿 access_token（Redis 缓存）
  │                           │ ─────────────────────────►
  │                           │ ◄───────────────────────── access_token
  │                           │ ④ POST /cgi-bin/qrcode/create（临时二维码，expire=120s）
  │                           │ ─────────────────────────►
  │                           │ ◄───────────────────────── ticket + url
  │ ◄─────────────────────── {scene_id, qr_url, expire_seconds}
  │
  │ ⑤ 展示 qr_url 图片，启动轮询
  │    GET /api/oauth/wechat-mp/status?scene_id=xxx
  │ ─────────────────────────►
  │                           │ status=pending
  │ ◄──────────────────────── {status:"pending"}
  │      （每 2s 重复）
  │
  │             [用户用微信扫码]
  │                           │ ◄───────────────────────── POST /api/mp/wechat/callback
  │                           │                                    (subscribe/SCAN 事件 XML)
  │                           │ ⑥ 签名校验 + 解析 XML 取 openid + scene_id
  │                           │ ⑦ Redis 标记 status=scanned + 写入 openid/nickname
  │                           │ ─────────────────────────►  回复 XML「扫码成功」
  │
  │ ⑧ 下一次轮询
  │ ─────────────────────────►
  │                           │ ⑨ 检测到 scanned → findOrCreateWeChatMpUser
  │                           │ ⑩ setupMpLogin：写 session、生成 access_token、删除 scene key（防重放）
  │                           │ ⑪ 异步推送公众号客服消息「登录成功！欢迎 xxx」
  │ ◄──────────────────────── {status:"logged_in", id, username, access_token, ...}
  │
  │ ⑫ 停止轮询，持久化 access_token + id
  │
  │ ⑬ 后续业务 API
  │    Authorization: <access_token>
  │    New-Api-User:  <id>
  │ ─────────────────────────►
```

---

## 3. 状态机

`GET /api/oauth/wechat-mp/status` 的 `data.status` 字段会按以下状态机流转：

```
                ┌─────────────────────┐
                │  pending (Redis 内) │ ← CreateMpSession 写入，TTL 180s
                └──────────┬──────────┘
                           │ 收到微信 SCAN/subscribe 回调
                           ▼
                ┌─────────────────────┐
                │  scanned (Redis 内) │ ← MarkScanned 写入 openid
                └──────────┬──────────┘
                           │ 下一次客户端轮询命中
                           │ （scanned 不会返回给客户端，会立即完成登录）
                           ▼
                ┌─────────────────────┐
                │  logged_in (一次性) │ ← setupMpLogin 返回后立即删除 Redis key
                └─────────────────────┘

  任何阶段 Redis key 过期/丢失  ─►  expired
  findOrCreateWeChatMpUser 失败 ─►  failed
  用户被封禁                    ─►  failed
```

**关键点**：客户端**永远不会收到 `status=scanned`**。服务端一旦检测到 scanned，就在同一个请求里完成用户创建/查找、session 写入，直接返回 `status=logged_in` 一次性响应，并立即删除 Redis key 防重放。客户端再次轮询同一 scene_id 会拿到 `expired`。

---

## 4. 接口速查表

| 接口 | 方法 | 路径 | 鉴权 | 限流 |
|---|---|---|---|---|
| 生成二维码 | POST | `/api/oauth/wechat-mp/qrcode` | 无 | CriticalRateLimit |
| 轮询登录状态 | GET | `/api/oauth/wechat-mp/status` | 无（scene_id 即凭证） | CriticalRateLimit |
| 公众号回调 | GET/POST | `/api/mp/wechat/callback` | 微信签名校验 | 无（微信服务器调用） |

> 注意：所有响应 HTTP 状态码都是 200，业务成败看 `success` 字段。这是项目惯例。

---

## 5. 接口详情

### 5.1 `POST /api/oauth/wechat-mp/qrcode` — 生成二维码

**请求**
```http
POST /api/oauth/wechat-mp/qrcode
Content-Type: application/json

{ "aff_code": "可选，邀请码" }
```

`aff_code` 可省略（body 可为空对象或不存在）。

**成功响应**
```json
{
  "success": true,
  "message": "",
  "data": {
    "scene_id": "sl_ABCDEFGHIJKLMNOPQRSTUVWXYZ123456",
    "qr_url": "https://mp.weixin.qq.com/cgi-bin/showqrcode?ticket=...",
    "expire_seconds": 120
  }
}
```

**失败响应**（部分场景）
```json
// 管理员未开启
{ "success": false, "message": "管理员未开启微信扫码登录" }

// 未启用 Redis
{ "success": false, "message": "微信扫码登录需要启用 Redis" }

// 未配置 AppId/AppSecret
{ "success": false, "message": "管理员未配置微信公众号 AppId/AppSecret" }

// 微信接口调用失败（网络/AppId 错误等）
{ "success": false, "message": "创建二维码失败：..." }
```

**字段说明**
| 字段 | 类型 | 说明 |
|---|---|---|
| `scene_id` | string | 扫码会话唯一标识，前缀 `sl_` + 32 位随机字符。**作为下一步轮询的凭证**，不要泄露给第三方 |
| `qr_url` | string | 直接可用的二维码图片 URL，`<img src="...">` 即可展示。来源于微信 `showqrcode` 接口，内容是 PNG 图片 |
| `expire_seconds` | int | 二维码有效期（秒），固定 120。客户端应据此设置过期定时器 |

### 5.2 `GET /api/oauth/wechat-mp/status` — 轮询登录状态

**请求**
```http
GET /api/oauth/wechat-mp/status?scene_id=sl_xxx
```

**响应（按状态分）**

**pending** — 等待扫码
```json
{
  "success": true,
  "message": "",
  "data": { "status": "pending", "scene_id": "sl_xxx" }
}
```

**logged_in** — 扫码成功，一次性返回（**客户端拿到后应立即停止轮询并持久化 token**）
```json
{
  "success": true,
  "message": "",
  "data": {
    "status": "logged_in",
    "scene_id": "sl_xxx",
    "id": 42,
    "username": "wechat_mp_42",
    "display_name": "张三",
    "role": 1,
    "user_status": 1,
    "group": "default",
    "access_token": "sk-xxxxxxxxxxxxxxxxxxxxxxxxxx"
  }
}
```

**expired** — 二维码已过期 / scene_id 不存在或已消费
```json
{
  "success": false,
  "message": "二维码已过期",
  "data": { "status": "expired" }
}
```

**failed** — 登录失败（如注册关闭、用户被封禁）
```json
{
  "success": false,
  "message": "管理员关闭了新用户注册",
  "data": { "status": "failed" }
}
```

**字段说明**
| 字段 | 类型 | 说明 |
|---|---|---|
| `status` | string | 状态机标识，取值见上文。**注意是字符串**，不是数字 |
| `id` | int | 用户 ID。非 cookie 客户端**必须**持久化，作为 `New-Api-User` header 的值 |
| `username` | string | 自动生成的用户名，前缀 `wechat_mp_`（区别于其他微信登录渠道的 `wechat_` 前缀） |
| `display_name` | string | 显示名，取自微信昵称；拉取失败时为 `WeChat User` |
| `role` | int | 角色。`1` 普通用户，`10` 管理员，`100` 超级管理员 |
| `user_status` | int | 用户启用态。`1` 启用，`2` 封禁。**注意不是 `status` 字段**，`status` 已被状态机占用 |
| `group` | string | 用户分组，用于计费比率 |
| `access_token` | string | **长期凭证**。客户端持久化后作为 `Authorization` header 调业务 API |

### 5.3 `GET/POST /api/mp/wechat/callback` — 公众号回调

**接入方不需要调用此接口**，它由微信服务器调用。文档列出仅为完整性：

- `GET`：微信首次验证服务器地址时调用，服务端校验签名后原样返回 `echostr`
- `POST`：用户扫码后，微信推送 `subscribe`（未关注用户）或 `SCAN`（已关注用户）事件 XML，服务端：
  1. 校验签名 `SHA1(sort([token, timestamp, nonce]))` == `signature`
  2. 解析 XML 取出 `openid` 和 `EventKey`（即 scene_id）
  3. 通过 access_token 调 `cgi-bin/user/info` 拉取昵称（失败不阻断）
  4. Redis 标记 `status=scanned` + 写入 `openid` + `nickname`
  5. 返回 XML 客服消息「扫码成功，请返回网页完成登录。」

---

## 6. 鉴权机制（重点）

### 6.1 为什么不用 cookie

默认前端 `web/default` 和 `web/classic` 与 new-api 后端同源，浏览器自动携带 session cookie，无需额外操作。

但**其他前端**通常遇到以下情况之一：
- **跨域**：扩展、桌面端、移动端 WebView 跟 new-api 不同源，cookie 默认不共享
- **SameSite 限制**：现代浏览器对第三方 cookie 限制越来越严
- **无浏览器环境**：Electron 主进程、Node CLI 等

此时应使用 **access_token** 模式：

### 6.2 两个必需的 header

```http
Authorization: <access_token>
New-Api-User:  <user_id>
```

- `Authorization`：值就是 `data.access_token`，**不需要** `Bearer ` 前缀（服务端会自动剥离 `Bearer `，所以加不加都行）
- `New-Api-User`：值是 `data.id` 转字符串，**必须带**，服务端会校验它和 token 持有者是否一致，不一致返回 401

### 6.3 验证 token 是否有效

调一个需要鉴权的接口，最常用的是获取自身信息：

```http
GET /api/user/self
Authorization: <access_token>
New-Api-User:  <user_id>
```

返回 `success:true` 即代表 token 有效。返回 401 或 `success:false` 表示 token 已失效（被重置、用户被删等）。

### 6.4 Token 生命周期

| 操作 | 是否影响 token |
|---|---|
| 扫码登录 | 生成 token（若用户首次登录） |
| 账密登录 | 生成 token（若用户此前没有） |
| `GET /api/user/token` | **重置 token**（旧 token 立即失效） |
| `GET /api/user/logout` | **仅清 session，不影响 token**。非 cookie 客户端调它没意义 |
| 用户在管理后台手动重置 token | 旧 token 立即失效 |
| 用户被删除 | token 失效 |

> **关键**：非 cookie 客户端**登出**需要自己在客户端清除本地存储的 `access_token` + `user_id`，调 `/api/user/logout` 对你来说基本是 no-op（它只清服务端 session，而你根本没用到 session）。

### 6.5 如何主动失效 token

如果客户端想「真正登出」（让当前 token 失效），可以：

```http
GET /api/user/token
Authorization: <当前 access_token>
New-Api-User:  <user_id>
```

这会**生成一个新 token 并返回**，旧 token 立即失效。客户端拿新 token 替换本地存储，再清空本地存储即可彻底登出。

---

## 7. 完整 TypeScript 接入示例

```typescript
// ============ 类型定义 ============
export interface WeChatMpQrCodeResp {
  success: boolean
  message: string
  data?: {
    scene_id: string
    qr_url: string
    expire_seconds: number
  }
}

export interface WeChatMpStatusLoggedIn {
  status: 'logged_in'
  scene_id?: string
  id: number
  username: string
  display_name: string
  role: number
  user_status: number
  group: string
  access_token: string
}

export interface WeChatMpStatusOther {
  status: 'pending' | 'expired' | 'failed'
  scene_id?: string
}

export interface WeChatMpStatusResp {
  success: boolean
  message: string
  data?: WeChatMpStatusLoggedIn | WeChatMpStatusOther
}

// ============ 基础 fetch 封装 ============
const API_BASE = 'https://your-new-api-domain'

// ============ 扫码登录流程 ============
const POLL_INTERVAL_MS = 2000

export async function getWeChatMpQrCode(affCode?: string): Promise<WeChatMpQrCodeResp> {
  const res = await fetch(`${API_BASE}/api/oauth/wechat-mp/qrcode`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ aff_code: affCode ?? '' }),
  })
  return res.json()
}

export async function getWeChatMpStatus(sceneId: string): Promise<WeChatMpStatusResp> {
  const res = await fetch(
    `${API_BASE}/api/oauth/wechat-mp/status?scene_id=${encodeURIComponent(sceneId)}`,
  )
  return res.json()
}

/**
 * 完整的扫码登录流程：调一次返回 access_token + user_id
 * 抛出 Error 表示流程失败（二维码过期、登录被拒、超时等）
 */
export async function loginViaWeChatMpScan(opts: {
  affCode?: string
  onQrCode?: (qrUrl: string, expireSeconds: number) => void  // 用于 UI 展示二维码
  onPending?: () => void                                      // 每次轮询到 pending 时回调
  signal?: AbortSignal                                        // 支持外部取消
  pollIntervalMs?: number
  timeoutMs?: number
}): Promise<WeChatMpStatusLoggedIn> {
  const {
    affCode,
    onQrCode,
    onPending,
    signal,
    pollIntervalMs = POLL_INTERVAL_MS,
    timeoutMs = 180 * 1000,  // 略大于 Redis TTL 180s
  } = opts

  // ① 生成二维码
  const qrResp = await getWeChatMpQrCode(affCode)
  if (!qrResp.success || !qrResp.data) {
    throw new Error(qrResp.message || '生成二维码失败')
  }

  const { scene_id, qr_url, expire_seconds } = qrResp.data
  onQrCode?.(qr_url, expire_seconds)

  // ② 启动客户端侧过期定时器（与二维码 TTL 对齐）
  const qrcodeDeadline = Date.now() + expire_seconds * 1000
  const globalDeadline = Date.now() + timeoutMs

  // ③ 轮询
  while (Date.now() < Math.min(qrcodeDeadline, globalDeadline)) {
    if (signal?.aborted) throw new Error('aborted')

    const statusResp = await getWeChatMpStatus(scene_id)

    if (!statusResp.success || !statusResp.data) {
      // expired / failed
      throw new Error(statusResp.message || '登录失败')
    }

    const data = statusResp.data
    if (data.status === 'logged_in') {
      // ✅ 拿到 token，直接返回（注意：服务端此刻已删除 scene_id，无法重复消费）
      return data
    }

    if (data.status === 'expired' || data.status === 'failed') {
      throw new Error(statusResp.message || '二维码已过期')
    }

    // pending → 继续等
    onPending?.()

    await new Promise((r) => setTimeout(r, pollIntervalMs))
  }

  throw new Error('轮询超时')
}

// ============ 使用示例 ============
async function main() {
  try {
    const userData = await loginViaWeChatMpScan({
      affCode: localStorage.getItem('aff') ?? undefined,
      onQrCode: (qrUrl) => {
        // 在 UI 上展示：document.getElementById('qr-img').src = qrUrl
        console.log('请扫码：', qrUrl)
      },
    })

    // ④ 持久化（用什么存储取决于你的运行环境）
    // 浏览器：localStorage / IndexedDB
    // 扩展：chrome.storage.local
    // 桌面：钥匙串 / 加密配置文件
    localStorage.setItem('access_token', userData.access_token)
    localStorage.setItem('user_id', String(userData.id))
    localStorage.setItem('username', userData.username)

    console.log(`欢迎 ${userData.display_name}，登录成功`)
  } catch (err) {
    console.error('扫码登录失败：', err)
    // 引导用户重试
  }
}

// ============ 后续业务请求 ============
async function callBusinessApi(path: string, init?: RequestInit) {
  const token = localStorage.getItem('access_token')
  const uid = localStorage.getItem('user_id')
  if (!token || !uid) throw new Error('未登录')

  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      ...(init?.headers ?? {}),
      Authorization: token,           // ← 不需要 'Bearer ' 前缀，加了也行
      'New-Api-User': uid,            // ← 必须带，与 token 持有者一致
    },
  })

  if (res.status === 401) {
    // token 失效，清理本地凭证并引导重新登录
    localStorage.removeItem('access_token')
    localStorage.removeItem('user_id')
    throw new Error('登录已失效，请重新登录')
  }

  return res.json()
}

// 示例：获取用户自身信息（验证 token 有效性）
async function getSelf() {
  return callBusinessApi('/api/user/self')
}

// 示例：主动失效当前 token（真正登出）
async function fullLogout() {
  // 这会重置 token，旧 token 立即失效
  await callBusinessApi('/api/user/token')
  localStorage.removeItem('access_token')
  localStorage.removeItem('user_id')
}
```

### 轮询参数建议

| 参数 | 建议值 | 说明 |
|---|---|---|
| 轮询间隔 | 2000 ms | 与服务端 `CriticalRateLimit` 友好；再快可能被限流 |
| 客户端二维码过期 | `expire_seconds * 1000` | 与微信临时二维码 TTL 对齐（120s） |
| 全局超时 | 180 s | 与 Redis session TTL 一致 |
| 退出条件 | `logged_in` / `expired` / `failed` / 超时 / 外部 abort | 任意一个触发即停 |

---

## 8. 用户名前缀与渠道识别

服务端 `/api/status` 返回三个前缀字段，用于在 UI / 数据分析中区分用户的微信登录渠道：

| 字段 | 前缀 | 渠道 |
|---|---|---|
| `wechat_login_username_prefix` | `wechat_` | 公众号验证码登录（关注公众号 + 回复验证码，老功能） |
| `wechat_oauth_username_prefix` | `wechat_` | 微信开放平台 OAuth（网页授权跳转） |
| `wechat_mp_username_prefix` | `wechat_mp_` | **本文档的扫码登录**（公众号带参二维码 + SCAN 事件） |

客户端可通过 `username.startsWith(prefix)` 判断用户来源，仅用于展示。**不要**把前缀作为请求参数或鉴权依据。

---

## 9. 错误处理与边界

| 场景 | 服务端表现 | 客户端处理 |
|---|---|---|
| 管理员未开启扫码登录 | qrcode 接口 `success:false`，message 提示 | 提示用户「暂未开放」，引导用其他登录方式 |
| Redis 未启用 | 同上 | 同上 |
| AppId/AppSecret 配错 | qrcode 接口 `success:false`，message 含微信错误码 | 联系管理员，提示「公众号配置异常」 |
| 公众号后台未配置服务器 URL | 二维码能生成，但扫码后状态永远 pending | 5 分钟超时后提示「未检测到扫码，请检查公众号配置」 |
| 用户 120s 内未扫码 | 二维码自然过期，客户端轮询拿到 expired | 展示「刷新二维码」按钮 |
| 用户扫码但网络抖动，回调丢失 | 同上，永远 pending | 同上 |
| 新用户但管理员关闭注册 | status 接口 `success:false`，message「管理员关闭了新用户注册」 | 提示「请联系管理员开通账号」 |
| 用户 openid 已存在但被封禁 | status 接口 `success:false`，message「用户已被封禁」 | 提示「账号已被封禁，请联系管理员」 |
| 重复轮询同一 scene_id（登录成功后再轮询） | 拿到 expired（Redis key 已删） | 客户端应保证 logged_in 后立即停轮询 |
| 跨设备扫码（A 生成二维码，B 知道 scene_id 轮询） | B 能拿到 A 的登录凭证 | scene_id 是 32 位随机串，实际不可猜测 |
| `New-Api-User` header 与 token 不匹配 | 业务 API 返回 401 `user id mismatch` | 检查 header 是否传对 |
| 网络抖动 | fetch 抛异常 | 轮询继续；登录后业务 API 给 2~3 次重试 |

---

## 10. 安全注意

1. **scene_id 是一次性凭证**：32 位随机字符前缀 `sl_`，碰撞概率 36^32。**不要写进日志**、不要让它在前端控制台长期可见。登录成功后服务端会立即删除 Redis key，重放攻击无效。
2. **access_token 等同密码**：
   - 浏览器扩展：用 `chrome.storage.local` 而非 `localStorage`（防 XSS）
   - 桌面端：用系统钥匙串（macOS Keychain / Windows Credential Manager）
   - 移动端：用平台提供的 secure storage
3. **必须 HTTPS**：scene_id、access_token 都是明文传输，HTTP 下可被中间人截获。
4. **轮询别太密**：接口有 `CriticalRateLimit`，2 秒间隔足够；更密可能被限流（表现为 HTTP 429 或 `success:false`）。
5. **关闭弹窗即停轮询**：避免后台僵尸定时器，浪费配额。
6. **不要把 token 暴露给第三方脚本**：如果接入方有插件市场 / 第三方扩展机制，access_token 必须在可信边界内流转。

---

## 11. 完整接入 checklist

**准备阶段**
- [ ] 联系管理员确认：Redis 已启用、`scan_login_enabled=true`、AppId/AppSecret/Token 配置正确
- [ ] 调 `GET /api/status` 确认 `wechat_mp_login=true`
- [ ] 公众号后台已配置服务器 URL 指向 `/api/mp/wechat/callback`、Token 一致、IP 白名单含服务端 IP

**接入开发**
- [ ] 实现 `POST /api/oauth/wechat-mp/qrcode` 调用，处理 `aff_code` 可选
- [ ] 在 UI 展示 `qr_url` 图片
- [ ] 实现 `GET /api/oauth/wechat-mp/status` 轮询，2s 间隔
- [ ] 正确识别 4 种状态：`pending` / `logged_in` / `expired` / `failed`
- [ ] `logged_in` 响应到达后**立即停止轮询**，持久化 `access_token` + `id`
- [ ] 实现客户端侧过期定时器（按 `expire_seconds`）
- [ ] 关闭弹窗 / 组件卸载时清理所有定时器
- [ ] 实现统一 fetch 封装：自动注入 `Authorization` + `New-Api-User` header
- [ ] 实现 401 自动登出（清本地凭证 + 引导重新登录）
- [ ] 区分用户渠道：`username.startsWith('wechat_mp_')` 仅做展示

**测试验证**
- [ ] 用微信测试号走通完整流程
- [ ] 测试「关闭弹窗后重新打开」：scene_id 应重新生成
- [ ] 测试「二维码过期后刷新」：能恢复
- [ ] 测试「封禁用户扫码」：拿到 failed 状态
- [ ] 测试「注册关闭时新用户扫码」：拿到 failed 状态
- [ ] 验证 `New-Api-User` 缺失或与 token 不匹配时业务 API 返回 401

---

## 12. 后端实现索引

| 文件 | 作用 |
|---|---|
| `controller/wechat_mp.go` | 3 个 HTTP handler：`WeChatMpQrCode` / `WeChatMpStatus` / `WeChatMpCallback`，以及 `findOrCreateWeChatMpUser` / `setupMpLogin` / `pushMpLoginSuccessMessage` |
| `service/wechat_mp/client.go` | 微信 API 客户端：`GetAccessToken`（Redis 缓存）、`CreateTempQRCode`、`GetUserInfo`、`SendCustomTextMessage` |
| `service/wechat_mp/callback.go` | 会话管理（`CreateMpSession` / `MarkScanned` / `MarkLoggedIn` / `DeleteMpSession`）、签名校验（`VerifySignature`）、XML 解析（`ParseMessage`）、事件处理（`HandleScanEvent`） |
| `service/wechat_mp/types.go` | 类型定义：`MpLoginState`、`WXMessage`、`UserInfo`、状态常量 |
| `setting/system_setting/wechat_oauth.go` | 配置：`ScanLoginEnabled`、`Token`、`EncodingAESKey` |
| `router/api-router.go` | 4 条路由注册（qrcode / status / callback GET / callback POST） |
| `controller/misc.go` | `GetStatus` 暴露 `wechat_mp_login` + 三个用户名前缀字段 |

### 关键常量

| 常量 | 值 | 位置 |
|---|---|---|
| `mpQRExpireSeconds` | `120` | `controller/wechat_mp.go` |
| `mpSceneIdPrefix` | `"sl_"` | `controller/wechat_mp.go` |
| `MpSessionTTL` | `180 * time.Second` | `service/wechat_mp/callback.go` |
| Redis key 前缀 | `wechat_mp:session:` | `service/wechat_mp/callback.go` |
| Redis access_token key | `wechat_mp:access_token:{appid}`（TTL 7000s） | `service/wechat_mp/client.go` |

---

## 13. 与其他登录方式的对比

| 方式 | 路径 | 体验 | 服务端依赖 | 是否跳转 |
|---|---|---|---|---|
| 账密登录 | `POST /api/user/login` | 输密码 | 无 | 否 |
| GitHub OAuth | 跳 github.com | 跳转 | OAuth App | 是 |
| 微信 OAuth（开放平台） | 跳 open.weixin.qq.com | 跳转 | 微信开放平台账号 | 是 |
| **微信公众号验证码** | 关注 + 回复 + 粘验证码 | 多步 | 第三方服务器（`common.WeChatServerAddress`） | 否 |
| **本文：微信公众号扫码** | 扫二维码自动登录 | 一步 | Redis + 公众号后台配置 | 否 |

本方案的核心优势：**二维码直接嵌入页面，扫码即登录，零跳转**。代价：需要在公众号后台配置服务器地址（一次性工作）。
