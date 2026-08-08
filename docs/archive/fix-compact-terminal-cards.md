# fix: 收紧命令与 Bash 终端卡片

- 变更时间：2026-08-09 02:46 (+08:00)

## 变更理由

命令执行卡收起态使用整块黑色背景，视觉重量过高；展开后的命令与回答中的 Bash 代码块也使用较大的标题栏和内边距，导致短命令占据过多纵向空间。

## 变更内容

- `apps/web/components/assistant-ui/rail.tsx`：移除收起态命令的黑色背景，压缩标题栏和展开区域，并复用统一代码块渲染，避免维护两套命令样式与复制逻辑。
- `apps/web/components/assistant-ui/markdown-text.tsx`：为 Shell/Bash 增加紧凑的 macOS 终端标题栏、三色状态点和深色正文，缩小字号、行高与间距；其他语言保留居中的语言标识。
- `apps/web/lib/long-running-command-ui.test.mjs`、`apps/web/lib/chat-message-layout.test.mjs`：覆盖白底命令标题、统一代码块复用和紧凑终端视觉契约。
- 关键取舍：三色圆点仅用于 Shell 语言，避免把终端装饰泛化到所有代码块；复制操作保持原有可访问名称与反馈。
