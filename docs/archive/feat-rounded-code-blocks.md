# feat: rounded assistant code blocks

- 变更时间：2026-07-03 00:51 (+08:00)

## 变更理由

Agent 回答里的代码块已经具备复制按钮和轻量语法高亮，但整体视觉仍偏硬，用户希望代码块 UI 更有圆角、更好看。

## 变更内容

- apps/web/components/assistant-ui/markdown-text.tsx：为代码块 header 和内容区分别补充顶部 / 底部圆角、边框与轻微阴影，让代码块呈现为一个完整的圆角卡片。
