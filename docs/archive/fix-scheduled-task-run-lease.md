# fix: 定时任务运行租约与过期清理

- 变更时间：2026-07-05 20:30 (+08:00)

## 变更理由

定时任务在 `TryStartScheduledTaskRun` 之后、`finishRun` 之前如果遇到 worker 崩溃或进程被杀，`scheduled_task_runs` 会永久停在 `running`。由于后续 claim 会拒绝同一任务下仍处于 `running` 的 run，interval 任务可能被 stale running run 永久阻塞，形成中间态死数据。

## 变更内容

- `apps/admin-api/internal/store`：新增运行 heartbeat 与 stale running 过期清理接口；复用 `scheduled_task_runs.updated_at` 表达租约，不引入新表结构。
- `apps/admin-api/internal/store/memory.go`、`apps/admin-api/internal/store/postgres.go`：实现 heartbeat 和过期清理；过期 run 标为 `error`，并更新任务的 `last_*` 与 `run_count`。
- `apps/admin-api/internal/service/scheduler.go`：每轮调度先清理超时 running；任务运行期间后台 heartbeat 刷新租约。
- `apps/admin-api/cmd/admin-api/main.go`：新增 scheduler heartbeat 和 lease timeout 环境变量。
- `apps/admin-api/internal/store/memory_test.go`：覆盖 heartbeat 防误清理、stale running 清理后 interval 可继续 claim、once 过期后不自动重试。
- 关键取舍：初版不做 exactly-once 和自动重放同一次 run，避免 worker 崩溃后重复向同一个 scheduled conversation 写入消息；interval 任务在下一轮自然继续。
