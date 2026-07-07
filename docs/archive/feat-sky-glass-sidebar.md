# feat: 用户侧收缩态玻璃侧边栏

- 变更时间：2026-07-07 23:16 (+08:00)
- 关联提交：待提交

## 变更理由

用户提供了天蓝色玻璃侧边栏设计稿，希望 cocola 用户侧 WebUI 吸收其中的收缩态 rail、透明玻璃质感和轻量云层氛围。上一版白色工作台侧边栏更偏常规面板，缺少设计稿中“背景透出”的玻璃感，也没有实现点击 icon 展开并定位到对应分区的交互。

## 变更内容

- apps/web/components/assistant-ui/app-sidebar.tsx：默认收缩为 64px sky-glass icon rail，点击 rail icon 先展开并定位到对应分区；展开态保留完整导航、会话、账户与调度能力。
- apps/web/app/globals.css：新增天空蓝 workspace 背景、透明玻璃侧边栏、高光/内阴影/blur，以及空会话云层氛围。
- apps/web/components/assistant-ui/thread.tsx：仅在空会话渲染装饰性云层，进入真实对话后保持消息阅读面干净。
- 校验：`pnpm --filter @cocola/web lint` 与 `pnpm --filter @cocola/web build` 通过。
