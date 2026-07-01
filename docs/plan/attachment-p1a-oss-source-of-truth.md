# Plan: 附件 P1a — MinIO 做真源 + 后端代 pull(送达仍 push)

状态：草案(待评审) · 日期：2026-07-01
关联：ADR-0017(附件存储分层 + 送达 push/pull 决策)、
docs/plan/web-file-upload.md §6、ADR-0008(持久化分层)、ADR-0007(BFF + gRPC 契约)

## 0. 前置事实(已核对)
- **dev compose 已内置 MinIO**:`deploy/docker-compose/docker-compose.dev.yml` 有
  `minio`(S3 API `:9000`、console `:9001`、凭据 `cocola/cocola_dev_pw`)与
  `minio-init`(自动 `mc mb local/cocola` 建桶)。**基础设施已就位,应用代码尚未使用。**
- gateway 侧 `chatRequest.Attachments`(`attachmentDTO{filename,content_b64,mime}`)、
  `agent.Attachment{Filename,Content,Mime}`、gRPC `QueryRequest.attachments` 均已存在(P0)。
- agent-runtime 侧 P0 已能把内联字节 push 进沙箱 `uploads/`(`_provision_into_sandbox` /
  无沙箱时 `_provision_onto_host`)。

## 1. 目标与非目标
### 目标
- 上传的附件**一律落 MinIO bucket** 作为 source of truth;消息只存 object key + 元信息。
- agent 运行前:小文件沿用内联直接写盘;大文件由**后端代 pull**(从 OSS 取字节)写进
  `uploads/`。**送达方式始终 push,agent 侧零改动、不新增工具面。**
- 阈值 `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`(默认 16MiB=16*1024*1024)可配置、不写死。

### 非目标(本 Plan 不做)
- 工具型 pull(给模型下载工具)—— P2 TODO。
- 历史消息附件回看 UI、presign 直传、分片/断点续传、bucket 生命周期与配额 —— 后续。
- 多模态图片走 Claude vision —— 独立方向。

## 2. 选型(按用户默认倾向拍板)
- **MinIO 客户端**:Go 侧 `github.com/minio/minio-go/v7`;Python 侧 `minio` 官方 SDK。
  两侧都用 MinIO 官方 SDK(贴 S3、依赖轻)。
- **谁落桶**:**gateway 落**(上传是 BFF 职责)。agent-runtime 保持"给我 key 我去取"
  的单一职责。
- **本地栈**:复用 dev compose 现成 `minio`/`minio-init`,不新增 service;full compose
  若缺 MinIO 则补齐同款配置。

## 3. 契约与数据流(P1a)
```
composer 选文件 → onNew 内联 base64 随 /v1/chat 带下去(前端契约不变)
  → gateway /v1/chat:
      1. base64 解码为字节(P0 已有)
      2. 【新】每个附件 PutObject 到 MinIO bucket=cocola,
         key = attachments/<session_id>/<uuid>-<sanitized-name>
      3. 【新】按 COCOLA_ATTACHMENT_INLINE_MAX_BYTES 分流,构造 gRPC Attachment:
         - 小文件(≤阈值):inline_content 带字节 + oss_key(便于回看)
         - 大文件(>阈值):只带 oss_key + size + mime,inline_content 置空
  → gRPC QueryRequest.attachments(proto 扩展,见 §4)
  → agent-runtime Query,provision 阶段:
      - 有 inline_content → 直接写盘(P0 路径)
      - 仅 oss_key → 【新】后端代 pull:MinIO GetObject 取字节 → 写进 uploads/
      - 两者都落到 <cwd>/uploads/<name>,前言清单不变
  → provider.query 正常跑(模型只是"读本地文件")
```

## 4. 契约变更(proto)
`packages/proto/cocola/agent/v1/agent.proto` 的 `message Attachment` 扩展(**加字段、不改旧序号**):
- `bytes content = 2;`(P0,复用为小文件 inline)
- 【新】`string oss_key = 4;`(对象存储 key;大文件必带,小文件可选便于回看)
- 【新】`int64 size = 5;`(原始字节数,用于阈值判定与日志)
→ 重新 `buf generate`(Go + Python 两侧 gen)。
> 分流判定在 gateway 完成;agent-runtime 只看"有没有 inline content",无需重算阈值。

## 5. 分步实施(每步独立可提交)
1. **proto**:`Attachment` 加 `oss_key`/`size` + 重生成 gen(Go/Py) —— 契约先行。
2. **gateway OSS 客户端**:新增 `internal/objstore`(minio-go 封装:PutObject/GetObject/
   健康检查);env `COCOLA_MINIO_ENDPOINT`/`COCOLA_MINIO_ACCESS_KEY`/
   `COCOLA_MINIO_SECRET_KEY`/`COCOLA_MINIO_BUCKET`/`COCOLA_MINIO_USE_SSL`。+ 单测(minio-go 可
   用 testcontainers 或 fake;优先 fake interface)。
3. **gateway chat 落桶 + 分流**:`/v1/chat` 解码后 PutObject,按
   `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`(默认 16MiB)决定 inline vs key-only,映射到 gRPC。
   + 单测(小文件带 inline、大文件仅 key)。
4. **agent-runtime 代 pull**:新增 MinIO 客户端(minio 官方 SDK);provision 阶段对"仅
   oss_key"的附件 GetObject 取字节再写盘;env 同源命名。+ 单测(fake objstore:
   inline 走旧路径、key-only 触发 pull)。
5. **compose/env**:确认 dev compose minio 就位;full compose 补 minio(若缺);
   gateway/agent-runtime service 注入 MinIO env 与阈值;`.env.example` 补全。
6. **端到端联调验收**:`make up-all`,分别传 <16MiB 与 >16MiB(需临时放宽前端上限或
   走大文件路径)文件,验证桶里有对象、沙箱 uploads/ 有文件、模型能读。
7. **文档**:回填 ADR-0017(P1a 落地)、每步配 docs/archive changelog。

## 6. 大小与安全约束
- object key 用 uuid 前缀防碰撞与枚举;filename 仍 sanitize 后作为落盘名与 key 后缀。
- bucket 默认私有(minio-init 里 `public` 前缀是 anonymous download 示例,附件不放 public)。
- MinIO 凭据仅经 env(ADR-0004);dev 默认 `cocola/cocola_dev_pw`,生产走 Vault/env。
- 阈值三处(前端 / gateway / agent-runtime)读同一配置源;前端上限提示与 gateway 硬校验一致。

## 7. 验收标准
- `make up-all`:小文件与大文件各一 → 桶 `cocola` 下 `attachments/<session>/...` 有对象;
  沙箱 `uploads/` 出现文件;模型能读到内容。
- 大文件不再内联进请求体/gRPC(仅 key 流转);小文件仍可内联。
- Go `go vet`/`go test`、Python `pytest`(用 .venv)、前端 lint/build 全绿。
- MinIO 不可用时:上传落桶失败发 error 事件不裸崩;代 pull 失败同样发 error。
- 每次提交配 docs/archive changelog;不改 SSE 契约与 send/cancel 能力面。

## 8. 风险与回退
- **新增外部依赖 MinIO**:dev 已内置;若 MinIO 挂,附件路径失败但主聊天链路不受影响
  (provision 失败仅影响带附件的请求)。
- **proto 加字段**:纯增量、旧序号不动,向后兼容;P0 内联路径保持可用。
- 回退:objstore 客户端可通过 env 缺省(未配 MinIO 时)回落到 P0 纯内联行为,便于灰度。
