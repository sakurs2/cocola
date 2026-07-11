# fix: 暴露 Session Checkpoint 保存与恢复失败

- 变更时间：2026-07-12 01:43 (+08:00)

## 变更理由

本地 `make dev` 退出时 Sandbox Manager 虽然进入 active-session checkpoint sweep，但
`CheckpointSession` 返回的 archive、MinIO 上传或 PostgreSQL 元数据错误被静默忽略。服务重启并
丢失原 Sandbox 后，Agent Runtime 又把“历史 session 没有 checkpoint”和“首次新 session”都处理
为普通未恢复，Environment prepare 只显示 Workspace ready，用户无法知道 Agent 已丢失历史状态。

## 变更内容

- `apps/sandbox-manager/internal/orchestrator/reaper.go`：关停 checkpoint sweep 返回扫描、成功、
  跳过和逐 session 失败结果，不再丢弃 provider 错误。
- `apps/sandbox-manager/cmd/sandbox-manager/main.go`：记录每个失败 session 的 sandbox、错误及 sweep
  汇总，MinIO/archive/PG 错误可直接从服务日志定位。
- `apps/sandbox-manager/internal/provider/checkpoint/checkpoint.go`：保留原始 checkpoint 错误，并同时
  暴露失败状态写库错误；成功元数据写入错误增加明确上下文。
- `apps/agent-runtime/cocola_agent_runtime/checkpoint.py`：恢复结果显式区分 `skipped`、`restored`、
  `missing`、`failed`。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：历史 session 缺少 checkpoint 或恢复失败时，
  Environment prepare 进入 degraded，并显示 Session restore 失败；首次新会话不误报。
- `scripts/run-stack-dev.sh`：将 OpenSandbox port-forward 隔离到独立进程组，避免终端 Ctrl+C 在
  Sandbox Manager 执行 checkpoint 前先杀掉转发；退出时先完成服务栈 checkpoint，再停止转发进程组。
- `scripts/run-stack.sh`：优雅退出按进程组判断存活并等待最多 30 秒，避免 `go run` 包装进程先退出后
  误判服务已经结束，继而在 Sandbox Manager 的 25 秒 checkpoint budget 内提前 SIGKILL 子进程。
- 测试：覆盖 checkpoint provider 错误汇总、关停预算耗尽、历史 checkpoint 缺失和对象恢复失败。

本修复保持 checkpoint 的 best-effort 资源回收语义，不引入功能开关，也不包含 durable chat 的
stash 变更。
