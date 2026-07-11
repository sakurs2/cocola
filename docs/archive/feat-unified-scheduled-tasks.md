# feat: 统一用户与 Admin 定时任务模型

- 变更时间：2026-07-11 13:30 (+08:00)

## 变更理由

原定时任务同时存在 Admin system task 与用户任务两套概念、两条执行链路和两套创建界面。用户需要理解 Interval/Cron 等技术配置，Admin 创建的无归属任务也无法自然进入具体用户的 Chat History。此次将任务收敛为始终归属于用户的一种产品模型，并提供简单的日历频率配置。

## 变更内容

- `db/migrations/00027_unified_scheduled_tasks.sql`：增加任务过期时间；把历史邮箱/用户名归属规范化为 Auth User ID；恢复可匹配的旧 system task Owner，暂停无法匹配的任务并等待 Admin 一次性指定。
- `apps/admin-api/internal/service`：新增 Once、Hourly、Daily、Weekly、Monthly 调度和月末补齐、过期清理、Owner 生命周期联动；所有任务统一以 Owner 身份通过 Gateway 和固定 Conversation 执行。
- `apps/admin-api/internal/store`：增加过期字段与幂等过期领取，并让用户任务权限只依赖 `owner_user_id`；保留 `owner_type` 数据库列用于滚动升级兼容，但不再序列化或参与执行分支。
- `apps/web/app/tasks`、`apps/web/components/scheduled-tasks`：新增用户 Tasks 卡片页和用户/Admin 共享 Drawer，提供 Today/All、五种简单频率、可选结束时间、暂停/恢复和结果入口。
- `apps/web/components/scheduled-tasks/task-drawer.tsx`、`apps/web/app/tasks`：让 Portal 内的 Drawer、Dialog 和操作菜单显式恢复用户或 Admin 主题作用域；日期时间控件限制未来时间和四位年份，并在提交前给出明确校验错误。
- `apps/web/app/admin/scheduled-tasks`：收敛为全站任务管理表格，仅保留搜索、状态筛选、只读详情和删除；任务配置、暂停和恢复由 Owner 在用户侧完成。
- `apps/admin-api/internal/httpapi`：Admin 定时任务接口只开放读取和删除，移除编辑、暂停/恢复和立即运行路由，避免前后端权限语义不一致。
- `apps/admin-api/internal/store/postgres.go`：修复 Once 任务以空 `next_run_at` 写终态时 PostgreSQL 无法推断参数类型的问题；scheduler 不再吞掉终态持久化错误。
- `apps/web/lib/scheduled-tasks.ts`：Today 只展示仍需关注的 Active/Paused 任务，Completed/Expired 任务保留在 All。
- `apps/web/components/assistant-ui`：用一级 Tasks 导航和命令面板入口替换旧 Schedule 弹窗，继续复用现有 Sky Glass Shell。
- `docs/frontend-tech-stack.md`：记录 Tasks 组件、身份执行和新旧频率兼容边界。
- 关键取舍：旧 Interval/Cron 任务继续按原配置执行，只有主动改选新频率时才转换；不新增任务表、Agent Proto 或前端依赖。
