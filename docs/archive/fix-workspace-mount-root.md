# fix: mount session workspace at /workspace

- 变更时间：2026-07-03 23:23 (+08:00)

## 变更理由

原先会话工作区已经由外部 host dir/PVC 按 `session_id` 隔离，但容器内仍挂载到
`/workspace/<session_id>`。这会把内部会话 ID 暴露给 agent，且在“一会话一工作区卷”的
现有模型下显得重复。更自然的语义是外部保持按 session 隔离，容器内统一呈现为
`/workspace`。

## 变更内容

- `apps/sandbox-manager/internal/provider/docker/docker.go`：把 session host dir 挂载到
  `/workspace`，容器 `WorkingDir` 同步改为 `/workspace`。
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`：把
  `cocola-session-<sid>` PVC 挂载到 `/workspace`，entrypoint 权限修正同步 chown
  `/workspace`。
- `apps/sandbox-manager` 与 `apps/agent-runtime` 测试：更新 workspace 路径断言。
- `docs/adr`、`docs/plan`、`docs/runbook`：同步当前工作区路径说明。

## 注意事项

- 外部持久化目录/PVC 名称不变，已有工作区数据不会因为挂载点变化而丢失。
- 历史 Claude session 如果记住旧的绝对路径 `/workspace/<session_id>`，可能需要新一轮对话
  重新建立路径上下文。
