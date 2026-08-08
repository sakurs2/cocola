# fix: 压缩命令卡片与 Project 任务页头

- 变更时间：2026-08-09 02:29 (+08:00)

## 变更理由

命令执行卡片把 `Finished` 等状态放在命令下方，造成标题区纵向松散；Project 任务页同时保留了工作区眉题和页面标题，使 `Projects` 与任务标题之间留白过大。

## 变更内容

- `apps/web/components/assistant-ui/rail.tsx`：将运行、完成和失败状态收敛为命令右侧的语义图标，保留可访问标签与悬停说明，移除重复状态文案。
- `apps/web/components/assistant-ui/workspace-shell.tsx`：仅对 Project 任务路由使用紧凑单行顶栏，其他工作区页面维持原布局。
- `apps/web/lib/long-running-command-ui.test.mjs`：覆盖命令状态图标、可访问语义和冗余文案清理。
- `apps/web/lib/project-task-ui.test.mjs`：覆盖 Project 任务路由的紧凑顶栏。
- 状态图标不是操作入口，因此使用原生 `title` 提示，不套用会要求可点击触发器的 HeroUI Tooltip。
