# feat: 历史对话恢复 Session Status

- 变更时间：2026-07-17 00:36 (+08:00)

## 变更理由

Session Status 原先只由新 Agent 会话的实时 `environment_status` 事件写入前端内存。重新打开历史对话时，页面只加载持久化消息，无法恢复当时已加载的 Skills 和 MCP 连接结果，因此右上角不再显示 Session Info。

## 变更内容

- `apps/gateway/internal/convo/store.go`：增加内部 `session-status` 消息部件，复用现有消息 JSONB 保存脱敏环境快照，不增加数据库迁移。
- `apps/gateway/internal/convo/reducer.go`：校验并原位更新最新 `environment_status`，保留未知组件字段，同时避免打断正文文本合并。
- `apps/gateway/internal/convo/reducer_test.go`、`apps/gateway/internal/httpapi/convo_test.go`：覆盖状态更新、非法输入忽略和完整聊天持久化。
- `apps/web/app/runtime-provider.tsx`：历史消息、本地缓存和 Run 重连快照加载时恢复最近一次 Session Status；内部状态部件不会渲染到聊天正文。
- `docs/frontend-tech-stack.md`：记录 Session Status 的持久化和历史恢复语义。

该方案不唤醒 Sandbox、不重新探测 MCP、不增加轮询或后台任务。升级前已经完成且没有保存状态快照的旧对话不会伪造状态；部署后产生过 `environment_status` 的对话可在后续打开时直接恢复。
