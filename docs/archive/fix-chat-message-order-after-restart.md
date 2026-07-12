# fix: 修复重启后用户与 Agent 消息反序

- 变更时间：2026-07-12 12:04 (+08:00)

## 变更理由

后台 Run 首次持久化 assistant 草稿时沿用了 Run 的开始时间，与同一轮 user 消息的
`created_at` 完全相同；最终 upsert 又没有更新时间。服务重启后 PostgreSQL 使用消息 ID
打破时间并列，而派生 ID 中 `-assistant` 排在 `-user` 前，导致历史对话显示为 Agent
先回答、用户后提问。

## 变更内容

- `apps/gateway/internal/httpapi/simple_chat.go`：草稿时间固定晚于本轮用户消息。
- `apps/gateway/internal/chatrun/postgres.go`、`apps/gateway/internal/convo/postgres.go`：
  assistant 从草稿更新为最终消息时同步更新 `created_at`。
- `apps/gateway/internal/convo`：相同时间戳按 user、assistant 的语义顺序读取，直接修复
  已经写入的历史数据，无需数据库迁移；内存与 PostgreSQL 实现保持一致。
- 补充相同时间戳排序和草稿转最终消息时间更新测试。

关键取舍：不新增消息序号字段或迁移。当前单会话单飞保证同一时刻只有一轮 Run，明确的
角色并列顺序足以修复历史数据，同时保持实现简单。
