# feat: Agent Knowledge、Suggested Prompts 与测试入口

- 变更时间：2026-07-28 14:50 (+08:00)

## 变更理由

领域 Agent 还需要清晰的远程知识入口和专属对话起点，但第一版不应引入文件下载、RAG、工作流或复杂权限模型。用户还需要在不自动发送消息的前提下预览提示词，并从编辑页安全地测试已保存 Agent。

## 变更内容

- `db/migrations/00053_agent_knowledge_prompts.sql`：为 Agent 增加飞书 Knowledge 引用和 Suggested Prompts JSONB 字段，分别限制 10 条和 4 条。
- `apps/gateway/internal/agentprofile/`：增加结构校验、HTTPS 飞书/Lark URL 安全解析和 Agent 快照持久化。
- `apps/gateway/internal/httpapi/agent_knowledge.go`：增加批量只读访问检查，固定请求官方开放平台域名，限制超时、响应体、重定向和并发，统一映射资源状态。
- `apps/agent-runtime/`：把 Knowledge 作为低优先级、不可信远程引用注入上下文，仅在任务相关时由对应 `lark-*` Skill 读取，不下载到 Sandbox。
- `apps/web/`：增加 Knowledge、Suggested Prompts 和 `Test Agent` 编辑能力；Agent 提示词只填充输入框，测试入口只打开全新会话且不自动发送。
- `apps/web/app/page.tsx`、`apps/web/app/runtime-provider.tsx`：支持查询参数预选 Agent，并在无效或归档时回退普通聊天。
- 关键取舍：Knowledge 权限状态不落库；默认 Skill 模式沿用当前有效 Skill，自定义模式自动补充所需 `lark-*` Skill。
