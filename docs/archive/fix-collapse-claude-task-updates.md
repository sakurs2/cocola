# fix: 聚合 Claude Task 更新节点

- 变更时间：2026-07-22 16:04 (+08:00)

## 变更理由

Claude Code 新版使用 `TaskCreate`、`TaskUpdate`、`TaskList` 和 `TaskGet` 管理计划。原有消息转换只识别 `TodoWrite`，导致每次任务更新都渲染成独立的 “Updated tasks” 工具节点，一轮回答中出现大量重复节点。

## 变更内容

- `deploy/sandbox-runtime/shim/agent_shim.py`：增加有界的 Claude Task 状态聚合器，将 Task 工具调用转换为单个稳定的 `todo-list` Progress 节点，并兼容旧 `TodoWrite`。
- `apps/agent-runtime/tests/test_agent_shim_mcp.py`：覆盖任务创建、更新、列表恢复、详情恢复和旧 TodoWrite 的消息转换行为。
- 聚合状态限制为最多 100 个任务和 256 个待处理调用，避免长会话状态无限增长。
