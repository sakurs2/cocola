# feat: Agent Todo 计划节点

- 变更时间：2026-07-22 14:53 (+08:00)

## 变更理由

Claude Code 和 Codex 在执行复杂任务时会维护 Todo/Plan 清单，但 Cocola 之前将进度快照渲染为普通工具调用，无法直观展示当前任务、待办任务和已完成任务，也会让 Claude Code 的 TodoWrite 同时出现进度与工具结果节点。

## 变更内容

- `deploy/sandbox-runtime/shim/agent_shim.py`：将 Claude Code TodoWrite 归一化为稳定的 progress 事件，并抑制对应的重复工具结果。
- `deploy/sandbox-runtime/shim/codex_adapter.mjs`：将 Codex todo_list 固定写入同一个 `todo-list` 进度节点，保证流式更新原位替换。
- `apps/web/lib/progress-items.mjs`：统一解析 Claude Code、Codex 和简单字符串清单的内容及状态。
- `apps/web/components/assistant-ui/rail.tsx`：新增 Plan 时间线节点，展示完成比例、已完成删除线、当前任务和待办状态。
- 实时对话、历史消息和只读分享页复用相同 Plan 渲染，并继续把 Plan 归入最终回答前的可折叠执行过程。
- 补充 Runtime 事件映射、进度传递、状态归一化和消息折叠边界测试。
- Runtime shim 发生变化，部署后需要重新构建 Sandbox Runtime 镜像。
