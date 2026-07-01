# Plan: 聊天附件上传(前端 → gateway → agent-runtime → 沙箱)

状态：草案(待评审) · 日期：2026-07-01
关联：ADR-0008(持久化分层)、web-product-ui-assistant-ui.md、
将新增 ADR-0017(附件存储分层 + 送达沙箱的 push/pull 决策)

## 1. 目标与非目标

### 目标
- 用户能在聊天 composer 里选文件、预览、随消息发送。
- 文件内容以「后端预置(push)」方式在 agent 运行前写入其沙箱工作区,
  模型可直接按路径读取。
- 复用 assistant-ui 现成的 attachment UI/adapter 体系,不自造上传组件。

### 非目标(本 Plan 不做)
- **B. 按需拉取(pull)**:给 agent 一个下载工具、由模型决定何时取文件 —— 留作 TODO。
- **OSS 持久化(P1)**:上传落 MinIO、消息存 object key、跨会话可回溯 —— 留作 P1,
  本 Plan 只做 P0。
- 大文件/分片上传、断点续传、多模态图片走 Claude vision —— 不在范围。

## 2. 决策(摘要,详见 ADR-0017)
- **真源归属(终态)**:MinIO 做 source of truth,沙箱工作区是一次性 working copy。
  依据:代码执行沙箱 ephemeral(ADR-0003 lease+GC;ADR-0008 T1a/T1b),
  只存沙箱会随会话蒸发,历史附件点不开。
- **送达沙箱方式**:选 **A. 后端预置(push)**,与 OpenAI Code Interpreter 挂载、
  E2B `files.write()` 一致;**B. pull 留 TODO**。
- **分阶段**:P0 先直接 WriteFile 进沙箱(不接 OSS)跑通链路;P1 再插入 MinIO 做真源。

## 3. P0 范围:直接 WriteFile 进沙箱(本次实现)

数据流:
```
composer 选文件
  → AttachmentAdapter.add()   预览(PendingAttachment)
  → AttachmentAdapter.send()  读成内容(P0: 文本/小文件内联 base64/text)
  → onNew 把附件[{filename, content}] 随 chat 请求带下去
  → /api/chat 透传
  → gateway /v1/chat: chatRequest 增 attachments 字段, 透传给 gRPC
  → agent-runtime Query: acquire 沙箱之后、provider.query 之前,
      先 exec `mkdir -p /workspace/<session_id>/uploads`(CopyToContainer 不建中间目录),
      再对每个附件 binder.write_file(sandbox_id, /workspace/<session_id>/uploads/<name>, content)
  → 在 prompt 前言里告诉模型:「用户上传了以下文件:./uploads/...(相对 cwd)」
  → provider.query 正常跑
```

### 3.1 契约变更
- **proto**(`packages/proto/cocola/agent/v1/agent.proto`)
  `QueryRequest` 新增 `repeated Attachment attachments = 6;`
  新增 `message Attachment { string filename = 1; bytes content = 2; string mime = 3; }`
  → 重新 `buf generate`(Go + Python 两侧 gen)。
- **gateway**(`apps/gateway/internal/httpapi/api.go`)
  `chatRequest` 增 `Attachments []attachmentDTO`(filename/content_b64/mime)。
  注意:`dec.DisallowUnknownFields()` 已开,字段必须显式加。
  `agent.Query`(`internal/agent/client.go`)增 `Attachments` 字段并映射到 gRPC。
- **agent-runtime**(`server.py` Query)
  在沙箱 acquire 成功后、`provider.query` 前,遍历 `request.attachments`,
  经 `self._binder.write_file(...)` 落到 `/workspace/<session_id>/uploads/<sanitized-name>`;
  (workspace 是会话隔离卷 ADR-0008 T1b,`/workspace` 根未挂载;写前需 `mkdir -p`)
  把落地路径清单拼进 `opts.system_prompt`(或 prompt 前言)。
  失败按现有约定发一个 `error` 终止事件,不裸崩流。

### 3.2 前端(`apps/web`)
- `runtime-provider.tsx`:给 `useExternalStoreRuntime` 增
  `adapters: { attachments: new CompositeAttachmentAdapter([...]) }`。
  P0 用内置 `SimpleTextAttachmentAdapter` + `SimpleImageAttachmentAdapter` 起步
  (内容内联);`onNew` 改为不仅读 text part,也收集 attachment part 的内容,
  组装成请求体的 `attachments` 字段。
- `thread.tsx`:在 Composer 里加 `ComposerPrimitive.AddAttachment`(回形针)
  + `ComposerPrimitive.Attachments` 渲染待发 chip(`AttachmentPrimitive.Root/Name/Remove`);
  UserMessage 里渲染已发附件(只读)。全部用官方 primitive,套现有设计令牌。
- `/api/chat/route.ts`:透传即可(body 已整体转发),无需改动。
- `lib/sse.ts`:无需改动(上行不走 SSE)。

### 3.3 大小与安全约束(P0)
- 单文件上限(建议 1MB,内联 base64)+ 总大小上限;超限前端拦截并提示。
- 文件名 sanitize(去 `..`/绝对路径/控制字符),固定落 `/workspace/<session_id>/uploads/` 下。
- accept 白名单:文本/代码/常见图片;二进制大文件 P0 不支持(P1 走 OSS)。

## 4. 验收标准(P0)
- `make up-web`(Echo)与 `make up-all`(真实模型)下:
  选一个 .txt/.md/.py 文件 + 输入问题 → 发送 → 沙箱 `/workspace/<session_id>/uploads/` 出现该文件
  → 模型能读到内容并在回答中引用。
- 前端 prettier / lint / build 全绿;Go `go vet`/`go test`、Python `pytest` 全绿。
- 附件写入失败时,前端渲染出 error 事件而非静默丢失/崩溃。
- 每次提交配 `docs/archive/` changelog;不改 SSE 契约与 runtime send/cancel 能力面。

## 5. 分步实施(每步独立可提交)
1. proto 加 Attachment + 重新生成 gen(Go/Py) —— 契约先行。
2. agent-runtime Query 预置写入 + prompt 前言 + 单测(用 fake binder)。
3. gateway chatRequest/Query 加字段透传 + 单测。
4. 前端 adapter + composer 回形针 UI + onNew 组装 + 校验。
5. 端到端联调验收(Echo → 真实模型)。
6. 写 ADR-0017,回链 ADR-0008,标注 B/OSS 为 TODO。

## 6. TODO(P1 及以后,不在本 Plan)
- [ ] B. 按需拉取工具(pull):给 agent 一个从 workspace/OSS 取文件的工具。
- [ ] OSS(MinIO)做真源:上传落 bucket、消息存 object key、agent 前从 OSS 拉进沙箱。
- [ ] 大文件/二进制/分片、presign 直传、bucket 生命周期与配额。
- [ ] 历史消息附件回看(依赖 OSS 持久化)。
