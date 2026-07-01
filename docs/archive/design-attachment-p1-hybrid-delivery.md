# design: 附件 P1 混合送达设计定稿(OSS 真源 + 16MiB 可配置阈值分流 + 后端代 pull)

日期：2026-07-01

## 背景
P0 已跑通「内联 base64 → 后端预置写进沙箱 `uploads/`」链路。用户就下一步提出两问:
1. 是否实现「上传落 MinIO → 消息存 OSS key → agent 用时拉进沙箱」,是否要给 agent 下载工具?
2. 后端预置能力保留,能否做成「小文件后端预置、大文件按需取」的混合。

## 决策(用户拍板)
- **真源换 MinIO**:上传一律落 bucket,消息只存 object key + 元信息,支撑历史回看。
- **按大小分流,阈值默认 16MiB 且可配置、不写死**:经
  `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`(默认 `16*1024*1024`)配置;前端 / gateway /
  agent-runtime 三处上限读同一配置源。
- **送达方式始终是 push**:阈值以下沿用 P0 内联+直接写盘;阈值以上消息存 key,
  agent 运行前由**后端代 pull**(取 OSS 字节)再 WriteFile 进 `uploads/`。
  对模型两条路都是「文件已在工作区、按路径读」,**agent 侧零改动、不新增工具面**。
- **工具型 pull(给模型下载工具)降级为 P2 TODO**:仅当出现模型按需访问超大文件/
  数据集的明确诉求再补;在此之前后端代 pull 已覆盖大文件送达。

## 改动(纯文档,无代码)
- `docs/adr/0017-attachment-storage-and-sandbox-delivery.md`
  - Decision:P1 从「push 唯一 + OSS 后插」细化为「OSS 真源 + 16MiB 可配置阈值分流 +
    后端代 pull」,新增 P2 标注工具型 pull。
  - Alternatives:方案 B(模型持下载工具)由「留 TODO」改述为「大文件改用后端代 pull 覆盖,
    工具型 pull 降级 P2」。
  - Consequences/Followups:拆成 P1a(OSS 真源)、P1b(阈值分流+代 pull)、P2(工具型 pull)。
- `docs/plan/web-file-upload.md`
  - §1 非目标、§2 决策摘要:对齐新口径(送达始终 push;大文件后端代 pull;工具型 pull P2)。
  - §6 重写为「下一步:P1 混合送达」,含 P1a/P1b 分步 checklist;新增 §7 P2 及以后 TODO。

## 非目标
- 本次仅定稿设计与文档,**未写任何代码**;P1a/P1b 实现待后续任务。
- 大文件/分片、presign 直传、bucket 生命周期与配额、历史附件回看 UI 仍为后续。
