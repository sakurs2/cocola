# feat: Wiki 改用 Tiptap 富文本 Markdown 编辑器

- 变更时间：2026-07-26 01:59 (+08:00)

## 变更理由

用户希望 Wiki 的 Markdown 页面从源码编辑/预览切换为接近 Notion 的所见即所得文档体验，并提供清晰的图标工具栏；同时要求侧边栏 Wiki 入口位于 MCP 下方。原 Monaco 方案偏向源码编辑，不符合新的交互预期。

## 变更内容

- `apps/web/components/assistant-ui/app-sidebar.tsx`：将 Wiki 导航项移动到 MCP 下方。
- `apps/web/components/wiki/wiki-markdown-editor.tsx`：接入 Tiptap 和官方 Markdown 双向扩展，提供加粗、斜体、下划线、删除线、高亮、标题、列表、任务清单、引用、代码、链接、图片、分割线及撤销/重做工具栏。
- `apps/web/components/wiki/wiki-markdown-editor.module.css`：增加文档画布、粘性工具栏、任务清单、代码块和表格样式，并覆盖移动端、键盘焦点与减少动态效果设置。
- `apps/web/components/wiki/wiki-workspace.tsx`：Markdown 文件默认使用富文本编辑器，继续复用显式保存、revision/ETag 冲突保护和未保存离开提示，不增加自动保存。
- `apps/web/lib/wiki-markdown-editor.test.mjs`：覆盖常用 Markdown、任务清单、图片和 GFM 表格的解析/序列化往返。
- `apps/web/package.json`、`pnpm-lock.yaml`：加入 Tiptap 核心、Markdown、编辑器扩展和 TableKit 依赖。
- 关键取舍：不提供文本对齐按钮，因为标准 Markdown 无法无损表达对齐；保留 Markdown 可往返的格式，避免保存后静默丢失。Tiptap 官方 Markdown 扩展当前仍为 Beta，已有文档中的 Markdown 注释不保证往返保留。
