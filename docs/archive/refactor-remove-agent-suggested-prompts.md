# refactor: 删除 Agent Suggested Prompts

- 变更时间：2026-07-28 21:25 (+08:00)

## 变更理由

Agent 的 Suggested Prompts 增加了额外配置、持久化和会话展示语义，但不是当前
领域 Agent 的必要能力。为保持产品简单，删除 Agent 专属提示词，只保留未选择
Agent 时的全局 Prompt Starters。

## 变更内容

- `apps/web/`：删除 Agent 创建与编辑请求中的 `suggested_prompts`、编辑区和相关
  类型；选择 Agent 后不展示全局 Prompt Starters，普通对话继续保留现有入口。
- `apps/gateway/internal/agentprofile/`：删除 Suggested Prompt 数据结构、校验、
  Agent JSON、会话快照和存储读写逻辑。
- `db/migrations/00055_drop_agent_suggested_prompts.sql`：通过独立迁移删除
  `agents.suggested_prompts` 列及约束，保留可回滚的 Down 迁移。
- 更新 Gateway 与 Web 测试，确保运行时代码不再依赖 Suggested Prompts。
