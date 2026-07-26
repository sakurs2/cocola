# fix: 修复飞书 Connector 无法建立长连接

- 变更时间：2026-07-26 19:06 (+08:00)

## 变更理由

用户完成飞书授权后，Connector 持续停留在 `connecting`，机器人私聊消息没有回复。Gateway 每次对账都因 `ClaimConnectors` 的 `RETURNING id` 同时匹配目标表和候选 CTE 而触发 PostgreSQL `42702 ambiguous_column`，导致连接租约从未被抢占，长连接和 inbox worker 均未启动。

## 变更内容

- `apps/gateway/internal/channel/feishu/postgres.go`：将候选 CTE 的 ID 显式命名为 `candidate_id`，消除 `UPDATE ... FROM ... RETURNING` 的列名歧义。
- `apps/gateway/internal/channel/feishu/postgres_test.go`：在真实 PostgreSQL 集成测试中覆盖 Connector 启用、租约抢占和 `connecting` 状态返回。
- 修复不修改已有飞书凭证或绑定关系；Gateway 重启后会自动重新抢占并建立长连接。
