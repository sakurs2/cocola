# feat: 完善 Project 任务工作区体验

- 变更时间：2026-08-09 01:53 (+08:00)

## 变更理由

Project 任务页存在头部信息重复、工作区初始宽度过大、任务分支不可命名、任务对话无法从 Chats 快速进入，以及长命令卡片无法同时完整查看命令和输出等体验问题。分支命名还需要覆盖远端冲突与并发创建，不能只依赖前端校验。

## 变更内容

- `apps/web/app/projects/`、`apps/web/app/page.tsx`：压缩任务页头部，移除重复任务标题，将工作区默认宽度调整到最小值，并在首条消息前提供任务分支输入。
- `apps/web/app/runtime-provider.tsx`、`apps/web/components/assistant-ui/`：把 Project 对话纳入 Chats 并使用独立图标和路由；命令卡片分区展示完整命令与输出，增加 Shell 高亮和复制操作。
- `apps/gateway/internal/project/`、`apps/gateway/internal/httpapi/`、`apps/gateway/internal/chatrun/`：传递并校验用户选择的任务分支，检查远端同名分支，保持已开始任务的分支不可变。
- `db/migrations/00061_project_task_branch_names.sql`：增加项目内任务分支的大小写不敏感唯一索引，以数据库约束处理并发创建竞争。
- `apps/web/lib/`、`apps/gateway/internal/project/service_identity_test.go`：补充 Project 交互、命令卡片和分支规则回归测试。
- 关键取舍：仍强制保留 `cocola/task-` 安全前缀；远端查询提供早期反馈，数据库唯一约束作为最终一致性保障。
