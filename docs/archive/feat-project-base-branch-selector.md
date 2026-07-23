# feat: Project 对话支持选择基础分支

- 变更时间：2026-07-23 19:14 (+08:00)

## 变更理由

Project 对话此前只能隐式使用仓库默认分支，用户无法在创建任务前确认或选择工作的基础分支。需要在对话框中提供明确的分支选择，同时保证任务创建后分支基线不可被前端随意更换，并避免后端拒绝无效分支时提前进入不可编辑的 Task 页面。

## 变更内容

- `apps/gateway/internal/project`、`apps/gateway/internal/httpapi`：增加仓库分支查询、基础分支校验和 Project Task 身份持久化。
- `apps/agent-runtime`、`packages/proto`：将基础分支随 ProjectContext 传入 Runtime，并按已校验分支初始化 Git 工作区。
- `apps/web/app/projects`、`apps/web/components/assistant-ui`：在 Project 对话框底部增加分支选择器，置于 Runtime 和 Model 之后；已有 Task 显示只读分支信息。
- `apps/web/app/runtime-provider.tsx`：仅在 Gateway 明确接受 Project Run 后进入 Task 页面，分支失效时保留可编辑的创建界面。
- 增加 Gateway、Runtime 和 Web 测试，覆盖分支读取、身份校验、基础分支传递及服务端接受后的导航行为。
- 分支只在首轮创建 Project Task 时选择；Task 创建后保持锁定，不提供在同一 Task 内切换基础分支的隐式行为。
