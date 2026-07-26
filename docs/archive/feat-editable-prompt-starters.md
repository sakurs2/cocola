# feat: 新增可编辑的 Prompt Starter

- 变更时间：2026-07-27 01:26 (+08:00)

## 变更理由

首页对话框下方的快捷入口原先会在点击后直接发送消息，用户无法先检查或调整 Prompt；Excel analysis 也缺少可交互的文件占位符。首版需要让快捷入口只负责填充输入框，并以轻量方式支持必填表格附件，同时避免模板显隐导致输入框上下跳动。

## 变更内容

- `apps/web/components/assistant-ui/thread.tsx`：将自动发送的快捷建议改为 Prompt Starter；支持内联文件槽位、文件选择与替换、缺少必填文件时拦截发送，以及稳定的模板区域布局占位。
- `apps/web/lib/prompt-starter.ts`：新增 Prompt Starter 文件槽位、文本布局、文件绑定和必填校验工具。
- `apps/web/lib/prompt-starter.test.mjs`：覆盖填充而不发送、文件槽位替换与恢复、文件类型校验、必填校验和布局稳定性。
- `apps/web/lib/base64-attachment-adapter.ts`：允许上传 XLSX 文件，并使用唯一附件 ID 支持同名文件替换。
- 关键取舍：沿用现有 Composer 与附件链路，不新增后端协议或模板管理产品层。
