# fix: Wiki 使用站内对话框替代浏览器弹窗

- 变更时间：2026-07-26 02:10 (+08:00)

## 变更理由

Wiki 的创建目录、创建 Markdown、重命名、删除、未保存切换，以及 Tiptap 的链接和图片输入仍使用 `window.prompt`、`window.confirm` 或 `window.alert`。这些浏览器原生弹窗无法匹配 Cocola 视觉体系，错误信息也不能与表单字段一起展示；从 Wiki 通过侧边栏或命令面板离开时还会再次出现浏览器确认框。

## 变更内容

- `apps/web/components/ui/action-dialog.tsx`：新增基于 Radix Dialog 的通用输入和操作确认组件，支持站内校验错误、危险/警告层级、键盘焦点和移动端宽度。
- `apps/web/components/wiki/wiki-workspace.tsx`：创建目录、创建 Markdown、重命名、删除和未保存切换全部改用受控站内对话框；上传文件选择也先经过站内未保存确认。
- `apps/web/components/wiki/wiki-markdown-editor.tsx`：链接和图片 URL 改用站内输入对话框，非法 URL 在对话框内提示，并支持显式移除已有链接。
- `apps/web/components/assistant-ui/workspace-unsaved-changes.tsx`、`app-sidebar.tsx`、`command-palette.tsx`：将同步 `window.confirm` 导航保护改成可延迟执行的站内确认流程；侧边栏会话操作错误改用站内错误 Toast。
- `apps/web/components/assistant-ui/workspace-toast.tsx`：增加错误状态和更长的可读时长，供侧边栏替代 `window.alert`。
- `apps/web/lib/wiki-dialog-ui.test.mjs`：防止 Wiki 和未保存导航逻辑重新引入 `window.alert`、`window.confirm` 或 `window.prompt`。
- 浏览器关闭、刷新标签页时仍保留标准 `beforeunload` 保护；浏览器不允许自定义页面对话框阻止标签页关闭。
