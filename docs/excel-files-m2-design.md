# Excel 网关文件上传（M2）技术设计

> 日期：2026-09-07 · 状态：**已上线**（v20260907-1715 / commit 16c895e8，生产验收 §10 全通过，偏差见下）
> 前置：M1 server tools 已上线（`docs/excel-server-tools-m1-design.md`，方案 A，工具循环/沙箱生产在用）
> 需求来源：`~/Downloads/capability-gap-analysis.md` §4.1、§9.3（S3 + S3-V）
> 结论：**网关自建文件服务**（原路线 B）+ image block 放行 + vision 路由 + 沙箱 /files/input 联动

---

## 1. 目标与非目标

### 目标

1. **S3 文件服务**：`/excel/v1/files` 四端点（Anthropic Files API 兼容最小子集，TokenAuth），
   文档类文件（pdf/csv/xlsx/docx/txt/md/json）网关存储；
2. **S3-V 多模态路由**：image block 白名单放行 + 非 vision 模型自动改路由到视觉模型；
3. **document 块转换**：消息里的文档引用转换为文本提示，告知模型文件路径与读取方式；
4. **沙箱联动**：code_execution 增加 `file_ids` 参数，执行前文件出现在沙箱
   `/files/input/<file_id>/<filename>`（只读），兑现 vendor 系统提示词的 INPUT_DIR 承诺；
5. TTL 自动清理（默认 30 天），裸机零运维（无外部对象存储、无 cron）。

### 非目标（明确不做）

- 不做 Anthropic Files API 的完整字段集（`purpose` 校验放宽、无 batch 端点）；
- 图片**不落**文件服务（直传 image block，S3-V 处理）；
- 不做对象存储（OSS/S3）——原 S3 spec 假设有云存储，生产为裸机，改本地磁盘（§3 偏差）；
- 不做二期项：图片说明代理（S3-V 第 4 条"表格截图 OCR 兜底"）、办公文档预览渲染；
- 不动共享 relay 管线（R2 红线延续）；端点全挂 `/excel/*`（R1）。

## 2. 与原 S3 spec 的偏差（裸机约束）

| # | 原 spec（§9.3） | 本设计 | 理由 |
|---|---|---|---|
| 1 | OSS/S3/COS + DB 元数据 | 本地磁盘 `<数据目录>/files/<uid>/<file_id>` + DB 元数据 | 生产无对象存储、零运维；单机磁盘足够（配额 §5 封顶） |
| 2 | 定期清理（隐含外部 cron） | 网关内置 goroutine ticker（6h 扫一次） | 裸机无 cron；与 options 体系一致 |
| 3 | 下载 302 签名 URL 或流式 | 直接流式返回 | 无 CDN 需求，签名 URL 徒增复杂度 |
| 4 | 沙箱"容器挂载 /files/input 只读" | 硬链接桥接：`rootfs/files/input/<file_id>/<filename>` | NativeRunner 是 chroot 不是容器；同分区硬链接零拷贝，rootfs 目录对 nobody 天然只读（0644/0755 root:root） |
| 5 | 图片经文件服务 | 图片走 image block 直传（S3-V 原文如此） | 与 vendor 行为一致 |

## 3. API 规格

统一鉴权：`TokenAuth`（与 /excel/v1/messages 相同，任何启用 token）。
错误形态：Anthropic 风格 `{"type":"error","error":{"type":"invalid_request_error","message":"..."}}`。

### POST /excel/v1/files （multipart 上传）

- 表单：`file`（必填）+ `purpose`（可填，仅记录，默认 `default`）；
- 限制：单文件 ≤20MB；类型白名单按扩展名：
  `pdf csv xlsx xls docx txt md json png jpg jpeg webp gif`（图片类型也允许存，
  供"发文档给沙箱 OCR"类场景，但正常图片走 image block）；
- 响应 `200`：`{"id":"file-<24hex>","filename":"...","size_bytes":123,"mime":"...","created_at":"ISO8601","expires_at":"ISO8601"}`
  （Anthropic 真实 id 形如 `file-3o4b...`；**不**用下划线，兼容 vendor 正则预期）；
- 配额（per-user）：文件数 ≤100、总量 ≤500MB，超限 400 带明确 message；
- 文件名消毒：取 `filepath.Base` + 去控制字符 + 限 200 字节；mime 按 `http.DetectContentType` + 扩展名映射。

### GET /excel/v1/files （列表）

- 响应：`{"data":[{...同上, "status":"active"}],"has_more":false,"next_page":null}`（Anthropic 分页形态；
  单页 ≤100 条，`limit`/`page` 参数 MVP 只支持首页，数据量到不了第二页）；
- 只返回**本人**文件（user_id 过滤）。

### GET /excel/v1/files/:id （下载）

- 归属校验：非本人 → 404（不泄露存在性）；文件缺失 → 404；
- 流式返回 `Content-Type: <mime>` + `Content-Disposition: attachment; filename=...`。

### DELETE /excel/v1/files/:id （删除）

- 归属校验同上；删除 DB 行 + 本体 + `rootfs/files/input/<file_id>` 链接目录；
- 响应 `{"id":"...","deleted":true}`。

## 4. DB 模型（三库兼容，Rule 2）

新表 `excel_files`，`model/excel_file.go`，注册进 `model/main.go` AutoMigrate：

```go
type ExcelFile struct {
    Id        string `json:"id" gorm:"primaryKey;type:varchar(40)"` // file-<24hex>
    UserId    int    `json:"user_id" gorm:"index"`
    Filename  string `json:"filename" gorm:"type:varchar(255)"`
    Mime      string `json:"mime" gorm:"type:varchar(128)"`
    SizeBytes int64  `json:"size_bytes"`
    Purpose   string `json:"purpose" gorm:"type:varchar(64)"`
    Status    string `json:"status" gorm:"type:varchar(16)"` // active | deleted
    CreatedAt int64  `json:"created_at" gorm:"index"`         // unix 秒
    ExpiresAt int64  `json:"expires_at" gorm:"index"`         // unix 秒
}
```

- 主键字符串网关生成（`crypto/rand` 24 hex），不用自增——三库行为一致；
- 软删（Status）不需要：DELETE 直接硬删行 + 本体（文件无审计需求）；
- 计数/容量查询走 `SELECT COUNT(*), COALESCE(SUM(size_bytes),0) WHERE user_id=?`——GORM 聚合，三库兼容。

## 5. 存储布局与生命周期

```
<数据目录>/files/<uid>/<file_id>          # 本体（uid 分目录，防单目录海量文件）
<数据目录>/sandbox/rootfs/files/input/<file_id>/<filename>   # 沙箱硬链接（执行时建立）
```

生命周期：上传落盘 + DB 行（expires = now + ttl_days）→ 引用/执行按需硬链 →
TTL 扫描（goroutine ticker 6h + 启动即扫一次）删过期：DB 行 + 本体 + 链接目录。
清理任务与上传/删除共用 per-file 删除函数（幂等）。

硬链接细节（同分区前提：数据目录与 rootfs 同在 `<数据目录>` 下，天然同分区）：
`os.Link(本体, rootfs/files/input/<file_id>/<filename>)`，已存在（EEXIST）视为成功（幂等）；
跨分区 fallback 复制（io.Copy，≤20MB 开销可接受）。
**链接不即时回收**：由 TTL/删除统一清理，避免并发执行窗口的竞态。

## 6. messages 内容块处理（excel_adapter 改造）

`excelSupportedBlockTypes` 现状：`text/thinking/tool_use/tool_result`。改造：

1. **image 放行**：白名单加 `"image": true`，结构 `{type:"image",source:{type:"base64",media_type,data}}`
   原样透传（`service/convert.go:159-165` 已支持 → OpenAI image_url，无需改动）；
2. **document 拦截转换**：新类型 `"document"` 不进白名单，在 `sanitizeExcelContentBlocks`
   内识别并**替换为 text 块**（不是丢弃）：
   ```
   [用户上传了文件「report.pdf」（大小 1.2MB）。
    文件已就绪：在 code_execution 工具调用中传 file_ids:["file-xxx"]，
    之后可在沙箱用 /files/input/file-xxx/report.pdf 读取（只读）。
    文档类内容请用 pdfplumber（PDF）/pandas 或 openpyxl（表格）/python-docx（Word）解析。]
   ```
   document 块形态兼容两种 source：`{type:"file",file_id}`（本服务）与
   `{type:"base64",media_type,data}`（客户端直塞——M2 不存盘，转换提示为
   "已收到内嵌文档，暂不支持，请通过上传入口使用文件服务"）；
3. **vision 路由（S3-V 第 2 条）**：清洗后检测 messages 是否含 image 块；
   含且当前模型别名 `vision != true` → `sanitized["model"] = GetVisionFallbackModel()`
   （第一个 Vision=true 的启用别名），并 `c.Set("excel_model_override", ...)` 供日志；
   无 vision 别名可用时保留原模型（上游报错自然暴露，不静默剥图）；
4. `X-Excel-Model-Override` 响应头：relay 写响应头的点位不可侵入（R2），
   MVP 用日志留痕（`excel: vision override u123 glm-x -> glm-4.6v`），头留二期。

模型别名结构 `ExcelModelEntry` 增加 `Vision bool`（默认 false，向后兼容旧 JSON）。

## 7. code_execution 联动

- `definitions.go` InputSchema 增加可选参数 `file_ids`（array of string），
  description 说明"传入本对话中上传文件返回的 file id 列表，文件将以只读方式出现在
  /files/input/<file_id>/ 下"；
- `codeexec.go` Execute：解析 `file_ids`（≤5 个/次）→ 逐个归属校验 + 存在性校验 →
  硬链进 rootfs（§5）→ 执行 → ModelText 不变；无效 file_id 在结果文本中明确报错
  （"file_ids 中 file-xxx 不存在或不属于你"），exit 正常；
- 安全面：路径全部由网关拼接（file_id 白名单字符集 `[a-z0-9-]` 校验 + filename
  来自 DB 消毒值），模型代码无法路径穿越到宿主（chroot 内 rootfs 即根）；
- nsjail 后端同样适用（rootfs 同一棵树，无 runner 特定逻辑）。

## 8. 配置与管理面板

`excel_tools.files` JSON（挂现有 excel_tools 配置组，后台面板加一个 TextArea）：

```json
{"enabled":true,"max_size_mb":20,"max_files_per_user":100,
 "max_mb_per_user":500,"ttl_days":30,
 "extensions":["pdf","csv","xlsx","xls","docx","txt","md","json","png","jpg","jpeg","webp","gif"]}
```

- `enabled:false` 时四端点返回 503 形态错误 + adapter 的 document 转换提示改为"文件功能未启用"；
- debug 端点（M1 已有 `/excel/api/tools/code_execution`）天然可用于联动验证。

## 9. 安全清单

| 风险 | 对策 |
|---|---|
| 越权下载/删除 | 所有 :id 操作 user_id 过滤；非本人返回 404 |
| 路径穿越 | file_id 字符集校验；filename 只取 DB 里的消毒值；存储路径网关拼接 |
| 磁盘打爆 | per-user 100 个/500MB + 单文件 20MB；类型白名单 |
| 恶意文件（webshell 等） | 文件只给沙箱读（禁网 + nobody + 无执行位 chmod 0644）；不给模型直读 |
| token 滥用刷接口 | 上传/下载走 M1 已有的通用限流（如无则二期补 per-user RPM） |
| 链接残留 | TTL 清理 + 文件删除时清链接目录 |

## 10. 测试与验收（对齐 §9.3/§9.3-V 原验收）

单测（darwin 可跑）：
- 文件名消毒、扩展名白名单、配额计算、file_id 字符集校验；
- document 块两种 source 的转换文本；
- vision 路由：含图 + 非 vision → 改写；纯文本 → 不动；vision 模型 → 不动；
- code_execution schema 含 file_ids。

线上验收（部署后 curl + 真机）：
1. curl 上传 PDF 得 file_id → code_execution 带 file_ids → 沙箱内
   `pdfplumber.open('/files/input/<id>/xx.pdf')` 读出页数（S3 原验收）；
2. 列表/下载/删除 + 越权 404；
3. GLM（vision）传图 → 模型描述图片（白名单 + 转换链路）；
4. DeepSeek（非 vision）传图 → 自动落视觉模型 + 图片被理解；
5. 纯文本对话零回归（现有 t0 探针抽 2 条）；
6. TTL：手动把一行 expires_at 改到过去 → 触发清理 → 本体/链接/DB 行消失。

## 11. 工作量

| 项 | 估算 |
|---|---|
| model + controller + router（四端点 + 清理任务） | 1 人日 |
| adapter（image 放行 + document 转换 + vision 路由 + 别名 Vision 字段） | 0.5 人日 |
| code_execution file_ids 联动 + 硬链桥接 | 0.5 人日 |
| admin 面板 + 配置 + 文档 + 测试 + 真机验收 | 1 人日 |
| **合计** | **3 人日** |

## 12. 待确认（评审点）

1. **document 块的实际形态**：vendor 前端恢复上传入口后会发什么块？
   本设计按 Anthropic 标准 `{type:"document",source:{type:"file",file_id}}` 兜底，
   联调时若形态不同只需改 `sanitizeExcelContentBlocks` 一处；
2. **图片是否也允许走文件服务**（本设计允许存，供沙箱 OCR 场景）——若产品不要可删 4 个图片扩展名；
3. **vision 回退模型**：当前接的 GLM 渠道是否有 vision 别名（glm-4.6 不确定，需渠道侧确认）；
   没有则 S3-V 的"DeepSeek 传图自动切换"暂不生效（保留机制，配置空则不切换）；
4. TTL 30 天 / 配额 100 个 500MB 的默认值是否符合产品预期。

## 13. 实施记录（2026-09-07 上线）

验收 §10 全部通过（生产 8.140.235.234，v20260907-1715 / commit 16c895e8）：
上传 CSV/PDF → 列表 → 下载 md5 一致 → 跨用户 404（下载/删除均不能越权）→
code_execution file_ids 沙箱内 pandas+pdfplumber 正常读取 → 坏 file_id 带内提示不中断 →
图片消息 GLM 正确识别颜色 → document 块提示注入后模型自发描述 file_ids 用法 →
纯文本探针零回归 → API 删除联动磁盘本体+沙箱链接 → TTL 回拨后清扫任务删净 DB 行/本体/链接。

与设计的偏差：
1. **TokenAuth 提前**：`/excel/v1` 组 TokenAuth 移到 ExcelRequestAdapter 之前（adapter 需
   `c.GetInt("id")` 做 document 属主判定；TokenAuth 不读 body，顺序无副作用）；
2. **vision 默认 true**：用户确认"当前对外的模型都支持图片"→ `Vision *bool` 三态，
   缺省=支持，存量模型列表配置零迁移即生效；
3. 评审点 2 维持"图片允许上传"（沙箱 OCR 场景）；评审点 3 当前生产模型列表全部
   vision=true，S3-V 切换机制就绪但未触发（无文本模型）。

