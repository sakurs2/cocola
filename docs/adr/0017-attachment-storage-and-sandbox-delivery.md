# ADR-0017: 聊天附件的存储分层与送达沙箱方式

- Status: Accepted
- Date: 2026-07-01
- Deciders: @cocola-maintainers
- Depends on: ADR-0007(gateway BFF + agent-runtime gRPC 契约)、ADR-0008(双卷持久化分层:代码执行沙箱 ephemeral、会话隔离卷 T1b)、ADR-0009(Route-A 沙箱内 agent)
- 关联 Plan: docs/plan/web-file-upload.md

## Context

用户需要在聊天里上传文件(文本/代码/小图),让沙箱内运行的 agent 能读到并据此作答。
这引出两个正交问题:

1. **真源归属**:文件的 source of truth 存哪?
2. **送达方式**:agent 运行在会话沙箱里,文件怎么进到它能访问的路径?

关键约束:
- 代码执行沙箱是 **ephemeral** 的(ADR-0003 lease+GC、ADR-0008 T1a/T1b):只存沙箱
  的东西会随会话蒸发,历史消息里的附件将点不开。
- 上行不走 SSE(SSE 契约只描述下行事件流,ADR-0007),附件必须搭 `POST /v1/chat` 请求体。
- JSON 无二进制类型;沙箱后端(OpenSandbox 主、docker 兜底)只暴露创建时挂卷 + 运行期
  WriteFile/ReadFile,无运行期热挂卷接口(ADR-0015)。
- 本 ADR **不解决** 大文件/分片/断点续传、多模态图片走 Claude vision、历史附件回看。

## Decision

**真源归属(终态)**:MinIO/对象存储做 source of truth,沙箱工作区是一次性 working copy。
依据沙箱 ephemeral,只有对象存储能支撑跨会话回看与重放。

**送达方式**:选 **A. 后端预置(push)** —— agent 运行前,后端主动把文件写进沙箱工作区
`/workspace/<session_id>/uploads/`,并在 prompt 前言告诉模型文件清单与相对路径。
对齐 OpenAI Code Interpreter 挂载、E2B `files.write()` 的成熟做法。

**分阶段**:
- **P0(本次已实现)**:直接 WriteFile 进沙箱,**不接 OSS**、文件内容内联 base64 走请求体
  跑通链路。契约:`POST /v1/chat` body 增 `attachments:[{filename,content_b64,mime}]`
  → gateway base64 解码为字节 → gRPC `QueryRequest.attachments`(`Attachment{filename,
  content bytes, mime}`)→ agent-runtime 在 acquire 后、query 前 `mkdir -p uploads` +
  `write_file` 落盘 + 前言。文件名 sanitize 防路径逃逸;单文件 1MB 上限 + accept 白名单;
  预置失败发终止 `error` 事件不裸崩。
- **P1(TODO)**:在 push 链路里插入 MinIO 做真源 —— 上传落 bucket、消息存 object key、
  agent 运行前从 OSS 拉进沙箱;支撑历史附件回看。

## Alternatives Considered

- **送达 B. 按需拉取(pull)** —— 给 agent 一个下载工具,由模型决定何时取文件。
  拒绝(留 TODO):P0 下模型未必知道该主动取、增加一轮工具调用延迟与失败面;push 语义
  更贴近"用户附了个文件"的直觉。未来若需按需大文件访问再补。
- **真源只存沙箱(不接 OSS)** —— 拒绝作为终态:沙箱 ephemeral,历史附件会蒸发。
  但作为 **P0 过渡** 接受,因为它最快跑通端到端、暴露契约问题,OSS 可后插不改上层。
- **附件走独立上传端点 + presign 直传** —— 拒绝(P0):内联 base64 对小文件足够简单,
  独立端点/presign 是 P1 大文件优化,过早引入徒增复杂度。
- **图片直接走 Claude vision 多模态 part** —— 拒绝(本 ADR 不做):P0 统一按文件落盘,
  多模态是独立方向。

## Consequences

- **Positive**:端到端链路以最小面跑通(契约、沙箱落盘、模型可读);对象存储可在
  push 链路里后插,上层前端/网关契约不必再改;沙箱内 agent 用"读本地文件"的自然姿态,
  无需新工具面。
- **Negative / 接受的风险**:P0 内联 base64 使请求体膨胀 ~33%,故设 1MB 硬上限,
  大文件在 P1 前不可用;P0 附件只活在沙箱内,会话结束即丢,历史消息附件点不开
  (P1 接 OSS 前的已知缺口)。
- **Followups(TODO,见 Plan §6)**:
  - [ ] B. pull 工具:给 agent 从 workspace/OSS 取文件的能力。
  - [ ] OSS(MinIO)做真源 + object key 存库 + 运行前拉入沙箱。
  - [ ] 大文件/二进制/分片、presign 直传、bucket 生命周期与配额。
  - [ ] 历史消息附件回看(依赖 OSS 持久化)。
