# feat: admin-managed agent system prompt

- 变更时间：2026-07-07 00:34 (+0800)

## 变更理由

管理员需要能为 agent 注入统一 system prompt，用于配置全局行为边界和默认工作方式。产品体验 v1 只暴露一个全局 prompt，但数据模型需要保留 scope、priority、version 等字段，方便后续扩展多 prompt 或分层策略。

## 变更内容

- `db/migrations/00024_agent_prompts.sql`：新增 `agent_prompts` 表，支持全局 prompt、版本、启用状态和排序字段。
- `apps/admin-api/internal/store/*`、`apps/admin-api/internal/service/agent_prompt.go`、`apps/admin-api/internal/httpapi/*`：新增全局 prompt 管理接口和 runtime-only effective 接口，audit 只记录启用状态、版本和内容长度。
- `apps/agent-runtime/cocola_agent_runtime/*`：新增 prompt loader，并在 Query 时按用户拉取有效 prompt 注入 system prompt，同时记录不含正文的 trace。
- `apps/web/app/admin/prompts/page.tsx`、`apps/web/app/api/admin/agent-prompts/[...path]/route.ts`、`apps/web/components/admin/admin-shell.tsx`：新增管理员 Prompt 页面，只暴露一个全局 prompt 的编辑、启用和保存体验。
- 测试覆盖 Go HTTP 全局 prompt/effective 流程、Python prompt loader 和 agent-runtime 注入行为。
