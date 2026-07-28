# feat: Agent 支持显式 Skill 集

- 变更时间：2026-07-28 14:50 (+08:00)

## 变更理由

现有 Agent 只能复用用户默认启用的 Skill，无法在不影响普通对话和其他 Agent 的前提下配置领域专用能力。需要让 Agent 可选取已有 Skill，同时保留“未选择时沿用默认 Skill”的简单语义，并确保管理员全局禁用始终不可绕过。

## 变更内容

- `db/migrations/00052_agent_skills.sql`：为 Agent 增加 `skill_ids` JSONB 字段，并限制最多 32 项。
- `apps/admin-api/`：增加用户安全 Skill catalog 摘要与 Runtime 内部 Skill 解析接口，覆盖归属、可用性、重复 runtime ID 和全局禁用校验。
- `apps/gateway/`：扩展 Agent 创建、更新、详情和会话快照；保存时校验新增 Skill，允许原样保留之后失效的已保存项。
- `packages/proto/`：在 `AgentContext` 中加入 Skill catalog ID，并重新生成 Go/Python 代码。
- `apps/agent-runtime/`、`deploy/sandbox-runtime/`：实现默认与自定义 Skill 两种运行模式、原生 Skill 白名单、Sandbox 残留清理、每轮显式 Skill 校验及 catalog 故障失败关闭。
- `apps/web/`：增加 Agent Skills 多选、状态文案、不可用提示与卡片标签；普通对话选择 `None` 时保持原行为。
- 关键取舍：会话保存 catalog ID 快照，但 Skill 内容、当前版本和管理员禁用状态仍在每轮动态解析。
