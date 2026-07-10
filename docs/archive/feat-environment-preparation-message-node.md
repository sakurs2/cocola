# feat: 消息时间线展示环境准备节点

- 变更时间：2026-07-11 02:29 (+08:00)

## 变更理由

新建对话或恢复历史对话到新 sandbox 时，checkpoint、附件和 Skill 注入发生在 Agent 推理之前。此前消息区没有对应过程，用户只能看到回答长时间未开始。复用仍然存活的 sandbox 时则没有必要增加额外节点。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/server.py`：仅在 Acquire 返回新 sandbox 时发送 `environment_prepare` 完整快照，在环境准备完成或降级时原位更新；快照不包含 MCP。
- `apps/gateway/internal/convo`：新增 `environment` part，使用 `json.RawMessage` 保存版本化快照，按稳定 `part_id` 更新并保持为消息第一个 part，未知未来字段原样保留。
- `apps/web/app/runtime-provider.tsx`、`apps/web/lib/environment.ts`：解析并容忍开放的 `schema_version + part_id + state + components[]`，未知 component kind/status 不导致整条消息丢失。
- `apps/web/components/assistant-ui/rail.tsx`、`thread.tsx`、`conversation-readonly.tsx`：增加与现有消息 Rail 一致的 Environment 节点，支持实时更新和历史回放。
- Environment 完成到首个真实回答 part 之间，由前端 running 状态派生临时的 `Starting response` Rail 节点，避免完成态产生“回答已结束”的错觉；节点不持久化，真实输出到达后自动消失。
- 当前 components 仅包含实际发生的 workspace、checkpoint、attachments 和 Skills；MCP 继续只在 Session Status 中展示。
- 不新增 Proto、数据库迁移、前端依赖、额外 sandbox 或模型调用。
