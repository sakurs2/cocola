# fix: 长 Shell 命令在终端卡片内换行

- 变更时间：2026-08-09 02:59 (+08:00)

## 变更理由

展开命令执行卡片时，Shell 命令使用不换行的 `white-space: pre`，长命令会产生横向溢出，并被卡片边界裁切，导致完整命令和右侧内容无法同时查看。

## 变更内容

- `apps/web/components/assistant-ui/markdown-text.tsx`：仅对 Shell/Bash 代码启用保留空白的自动换行和长路径断行，避免横向溢出；其他语言代码继续保留横向滚动，避免改变源码排版语义。
- `apps/web/lib/chat-message-layout.test.mjs`：补充终端命令换行策略的回归断言。
- 折叠态命令摘要仍保持单行省略，展开态展示完整命令。
