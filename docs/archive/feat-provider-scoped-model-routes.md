# feat: Provider 级模型路由与管理页重构

- 变更时间：2026-07-13 20:35 (+08:00)

## 变更理由

模型 alias 原先是全局主键，因此不同 Provider 无法暴露相同的用户侧模型名称；同时
Claude Code 与 Codex 共用一个默认模型会混淆 Messages 和 Responses 两种协议。
Admin Models 页面还把 Provider、模型和高级字段长期铺在同一页面，配置层级不清晰。

## 变更内容

- `db/migrations/00035_model_route_identity.sql`：为模型路由增加不可变 ID 和协议，alias
  改为仅在 Provider 内唯一；Run 与定时任务持久化实际使用的 route ID。
- `apps/admin-api`：模型 CRUD 改按 route ID，默认模型按协议独立维护，并禁止在仍有
  路由时改变 Provider 协议；删除仍有关联路由的 Provider 时返回可执行的冲突提示。
- `apps/llm-gateway`、`apps/gateway`、`apps/agent-runtime`：执行链路统一传递 route ID；
  旧 alias 仅在唯一可判定时兼容解析，避免同名模型被静默路由到错误 Provider。
- `apps/web`：对话与定时任务按 route ID 选择模型；Admin Models 改为 Provider/模型
  两个全宽列表，通过抽屉完成创建和编辑；上游 API 显式区分 Anthropic Messages、
  OpenAI Chat Completions 和 OpenAI Responses，并独立展示 Runtime 兼容性；输入控件不再
  使用高亮蓝色 focus ring。
- 测试补充同 alias 跨 Provider、同 Provider 冲突、每协议默认模型和真实 PostgreSQL
  迁移/Store parity 场景；功能默认启用，不增加灰度开关。
