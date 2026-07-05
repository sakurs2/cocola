# feat: 用户级 SSE 事件通道

- 变更时间：2026-07-05 18:35 (+08:00)

## 变更理由

用户定时任务可能在浏览器打开很久后才触发，前端轮询不适合这种低频、长等待场景。需要服务端主动推送任务开始和完成事件，让 chat history 无需刷新即可出现定时任务会话，并复用普通会话的 loading / 完成提示。

## 变更内容

- `apps/admin-api/internal/service/user_events.go`：新增通用 `UserEvent` envelope、内存事件 broker、用户事件 snapshot。
- `apps/admin-api/internal/redispub/redispub.go`：复用 Redis 增加 `cocola:user-events` pub/sub，支持多 admin-api 实例广播用户事件。
- `apps/admin-api/internal/service/scheduler.go`：用户定时任务运行开始、成功、失败时发布 `scheduled_task.run.*` 用户事件。
- `apps/admin-api/internal/httpapi`：新增 `/me/events` SSE，连接建立时先发送 snapshot，再转发当前用户的实时事件。
- `apps/web/app/api/events/route.ts`：新增同源 SSE 代理，浏览器无需直接暴露 admin-api 地址和 runtime token。
- `apps/web/app/runtime-provider.tsx`：监听用户事件并复用现有 `runningSessionIds` / `unreadCompletedSessionIds` 驱动 sidebar loading 与绿色完成勾。
- `apps/web/app/runtime-provider.tsx`：将 snapshot 视为定时任务 running 状态的权威对账，避免浏览器漏掉 finished/failed 事件后侧边栏和对话页永久显示 answering。
- `apps/web/app/runtime-provider.tsx`：snapshot 不再创建或置 running 定时任务会话，只有实时 `user_event` started 可以插入侧边栏；避免历史 stale running 数据让用户删除后的会话重新冒出。
- `apps/admin-api/internal/service/user_events.go`：snapshot 忽略已被任务 `last_run_at/last_status` 证明完成的 stale running run，并对超过 2 小时未更新的 running run 做保守清理，防止历史脏数据让页面初始态一直转圈。
- `apps/admin-api/internal/service/scheduled_tasks.go`：一次性任务的执行时间如果已经过去，创建/更新时直接返回错误，避免任务创建成功但 `next_run_at` 为空、调度器永远不会触发。
- `apps/admin-api/internal/service/user_events.go`、`apps/web/app/runtime-provider.tsx`：snapshot 带上 run 时间戳；前端可恢复新鲜 running 任务，同时用删除时间挡住旧 snapshot 把刚删除的会话重新插回侧边栏。
- `apps/web/components/assistant-ui/app-sidebar.tsx`、`apps/web/app/admin/scheduled-tasks/page.tsx`：Once 任务切换时默认填入 5 分钟后的本地时间；如果选择了 30 天后的时间，提交前展示完整年月日确认，避免误选到下一年后看起来像“到时间没运行”。
- `apps/web/components/assistant-ui/app-sidebar.tsx`：用户侧定时任务的频率/时间提示、远期 Once 确认、删除确认不再使用浏览器原生弹窗，改为与 Schedule 面板一致的页面内弹窗。
- 关键取舍：Redis pub/sub 只负责实时事件，不承担历史可靠投递；断线恢复依赖 DB snapshot 和 conversation 持久化的最终一致性。
