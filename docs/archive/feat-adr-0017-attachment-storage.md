# docs: ADR-0017 附件存储分层与送达沙箱方式

日期：2026-07-01 · 关联 Plan：docs/plan/web-file-upload.md(Step 6/6)

## 改动
- 新增 `docs/adr/0017-attachment-storage-and-sandbox-delivery.md`:
  - 真源终态定 MinIO/对象存储,沙箱工作区为一次性 working copy(依据沙箱 ephemeral)。
  - 送达方式选 **A. push(后端预置)**,对齐 OpenAI Code Interpreter / E2B;
    **B. pull 留 TODO**。
  - 分阶段:P0 直接 WriteFile 进沙箱 + 内联 base64(已实现);P1 插入 OSS 做真源。
  - 回链 ADR-0007/0008/0009;Followups 列 pull、OSS、大文件、历史回看。
- `docs/adr/README.md`:Index 增 0017 行。

## 校验
- 纯文档,无需构建;链路实现见 Step 1-4 提交,自动化验收见 Step 5。

## 非目标
- B. pull、OSS 持久化、大文件/分片、历史附件回看均为 P1+(见 ADR Followups)。
