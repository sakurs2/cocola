# feat: Agent Knowledge 动态同步与沙箱搜索

- 变更时间：2026-07-28 20:40 (+08:00)

## 变更理由

Agent 的 Instructions、Skills 等行为配置需要继续随会话快照固定，但 Knowledge
需要支持在同一会话的下一条消息读取最新配置。原有方案把 Knowledge 引用注入
System Prompt，来源增多后会持续占用上下文，也无法优雅处理增量更新、访问失败和
旧版本回退。

## 变更内容

- `db/migrations/00054_agent_knowledge_revision.sql`、`apps/gateway/internal/agentprofile/`：
  为 Agent Knowledge 增加独立 revision，仅在来源配置变化时递增。
- `packages/proto/cocola/agent/v1/agent.proto`、`apps/gateway/internal/httpapi/`：
  每条消息读取 Agent 的最新 Knowledge；行为仍使用会话快照，数据库短时失败时仅
  回退到快照中的最近有效 Knowledge。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：将 Knowledge 按 revision
  增量物化到沙箱，通过 staging 和原子切换更新 `current`；临时失败保留最近有效
  版本，永久不可用来源不继续暴露旧内容；切换失败会删除未激活的完整 revision，
  已成功切换后即使旧版本清理失败也不会错误回退状态。
- `deploy/sandbox-runtime/cocola_knowledge.py`、`deploy/sandbox-runtime/Dockerfile`：
  预装固定版本并校验摘要的 `ripgrep-all`，提供受控的
  `cocola-knowledge status/search/read`；搜索禁用 rga 自身缓存并限制路径、时间、
  来源数和输出大小；超时或输出超限时终止完整子进程组，XLSX 支持按工作表和有界
  单元格区域读取。
- `deploy/sandbox-runtime/skills/cocola-knowledge/`：增加内置 Knowledge Skill，
  让 Agent 在相关任务中先搜索再按需读取，不再把 Knowledge 正文或来源列表注入
  Prompt。
- `apps/web/components/agents/agent-capabilities-editor.tsx`：明确提示保存后的
  Knowledge 修改会从下一条消息起对已有会话生效。
- 相关 Gateway、Runtime、CLI 与 Web 测试覆盖 revision、快照回退、原子物化、
  路径逃逸、远程缓存、权限失败、固定字符串搜索、子进程清理、XLSX 区域读取和
  产品文案。
