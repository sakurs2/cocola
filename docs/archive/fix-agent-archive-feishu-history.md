# fix: 消除 Agent 归档竞态并隔离飞书历史对话

- 变更时间：2026-07-27 22:54 (+08:00)

## 变更理由

Agent 的 active 状态原先在资源写入事务之外校验。数据库并发下，Agent 可能在飞书注册完成、手工连接器写入或对话 Run 创建前被归档，留下无法通过正常管理接口处理的连接器，或允许已归档 Agent 继续创建资源。

飞书 `/history` 原先只按对话类型、Project 和 Runtime 过滤，没有按当前 Connector 的 Agent 过滤。用户可能切换到基础对话或其他 Agent 的对话，下一条消息确定性触发 `AGENT_MISMATCH`，并被当作临时错误重复重试。

## 变更内容

- `apps/gateway/internal/agentprofile/postgres.go`：归档事务先锁定 Agent，再检查飞书连接器和进行中的注册流程，最后更新状态。
- `apps/gateway/internal/channel/feishu/postgres.go`：创建注册流程、写入连接器和完成注册时，在同一事务内锁定并校验 Agent 仍为 active。
- `apps/gateway/internal/chatrun/postgres.go`：创建新对话或新 Run 前，在同一事务内锁定并校验 Agent 状态；已有请求的幂等重放仍优先返回原 Run。
- `apps/gateway/internal/channel/feishu/manager.go`：`/history` 和 `/switch` 仅接受当前 Connector 所属 Agent 的对话；`AGENT_MISMATCH` 改为一次性用户提示，不再进入重试队列。
- `apps/gateway/internal/httpapi/`：将事务内发现的 Agent 归档统一映射为 `409 AGENT_ARCHIVED`。
- 相关 Go 测试覆盖进行中注册阻止归档、已归档 Agent 拒绝资源写入、飞书历史 Agent 隔离及确定性错误不重试。

## 关键取舍

- 使用 PostgreSQL 行锁和显式事务确定资源创建与归档的先后顺序，不增加后台补偿任务或隐藏重试逻辑。
- 不修改 Agent/飞书产品模型，也不引入配额；本次仅修复审查中的问题 1 和问题 3。
