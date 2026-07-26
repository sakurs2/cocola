# feat: 用户级飞书 Connector

- 变更时间：2026-07-26 16:41 (+08:00)
- Review 修复时间：2026-07-26 18:12 (+08:00)

## 变更理由

用户希望在不经过 Cocola 管理员配置的情况下，为自己的账号连接独立飞书应用机器人，并在飞书私聊中复用现有 Cocola Agent 对话能力。正常连接流程应通过飞书授权链接自动取得应用凭证和用户标识；已有应用仅作为高级降级入口。

## 变更内容

- `db/migrations/00048_user_feishu_connector.sql`：新增用户级 Connector、注册流程、飞书会话映射和持久化收件箱表，包含用户/应用唯一约束、租约、幂等事件 ID 和终态清理所需索引。
- `apps/gateway/internal/channel/feishu/`：基于飞书 Go SDK v3.9.9 实现自动应用注册、手工应用绑定、长连接管理、数据库租约、所有者私聊校验、消息去重与串行处理、Agent SSE 转发、问题回答、命令和有限附件下载。
- `apps/gateway/internal/secretbox/`、`apps/gateway/internal/project/crypto.go`：提取通用 AES-GCM SecretBox；GitHub 与飞书继续共享 `COCOLA_SCM_SECRET_KEY`，并通过 AAD 隔离不同用户、Connector 和字段。
- `apps/gateway/internal/httpapi/feishu_connectors.go`、`apps/gateway/internal/httpapi/api.go`、`apps/gateway/internal/httpapi/projects.go`：新增用户鉴权的飞书 Connector API，并将飞书状态加入 Connector 聚合查询。
- `apps/gateway/cmd/gateway/main.go`、`apps/cli/internal/assets/compose.yaml`：接入飞书连接管理器的启动、停止和内部 Admin API 地址。
- `apps/web/components/connectors/feishu-connector-card.tsx`、`apps/web/app/api/connectors/feishu/`、`apps/web/app/connectors/page.tsx`：新增授权链接、有限轮询、审批提示、暂停/启用/解绑和已有应用 Dialog；全部使用项目 Dialog、Input 和 Toast，不使用浏览器原生弹窗。
- 测试覆盖可信账号换签、账号禁用、确定性 Run 请求 ID、SSE 解析、问题选项、受限媒体读取、Secret AAD 隔离，以及 Web 轮询清理和固定代理路由。

关键取舍：

- 复用现有 `/v1/chat` 和 Conversation 链路，不新增 Chat Service、消息队列、Redis、Outbox 或管理员配置。
- 飞书回调只完成校验与持久化入队；每个 Connector 串行执行，普通等待消息最多 5 条，附件最多 8 个且合计不超过 32 MiB。
- 解除连接删除本地密文凭证、绑定及 Channel Session，但保留 Cocola Conversation 历史，也不代替用户删除飞书侧应用。

## Review 修复

- `apps/gateway/internal/channel/feishu/managed_ws.go`、`runtime_sdk.go`、`manager.go`：保留官方飞书协议、事件解析、HTTP Client 和 StreamController，替换不可取消的 SDK WebSocket 生命周期，并避免构造会遗留清理协程的完整 Channel；重连后重新触发 ready，停止时等待连接协程退出，业务消息失败不再断开整条连接，并限制未完成分片占用。
- `apps/gateway/internal/channel/feishu/postgres.go`：普通消息严格按队头串行，延迟重试不会被后续普通消息越过；高优先级 `/stop` 仍可抢占。
- `apps/gateway/internal/channel/feishu/chat_client.go`、`manager.go`：`/stop` 使用当轮可信账号新签 Token 查询并同步取消真实 Run，不再保存或异步复用旧 Token。
- `apps/gateway/internal/channel/feishu/manager.go`：恢复 `/v1/chat` 幂等重放的 `snapshot` 文本和待回答问题，避免重试后只显示“任务已完成”。
- `apps/gateway/internal/channel/feishu/*_test.go`：新增 WebSocket 重连/停止、分片上限、快照恢复、新 Token 取消和真实 PostgreSQL FIFO 集成测试；Feishu race detector 与 Gateway 全包测试通过。
