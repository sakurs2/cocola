# feat: Agent Knowledge 支持 Cocola Wiki

- 变更时间：2026-07-28 15:13 (+08:00)

## 变更理由

Agent Knowledge 已支持飞书远程引用，但 Cocola 本身已有按用户隔离的 Wiki、不可变文件版本、对象存储和 Sandbox 物化链路。用户希望在不引入共享 Agent、复杂 ACL、RAG 或新 Skill 的前提下，让个人 Agent 直接使用自己的 Cocola Wiki 文件。

## 变更内容

- `apps/gateway/internal/agentprofile/`：Knowledge 来源新增 `cocola_wiki` 类型，以规范化 UUID `node_id` 标识文件；飞书 URL 与 Cocola Wiki 节点互斥并使用统一来源键去重。
- `apps/gateway/internal/httpapi/agents.go`：保存新来源时使用当前租户和用户身份校验 Wiki 文件；已保存后删除的文件允许原样保留，避免编辑 Agent 其他字段时被强制清理。
- `apps/gateway/internal/httpapi/agent_knowledge.go`：Knowledge 检查直接复用本地 Wiki Store，返回 `ready`、`not_found` 或 `temporarily_unavailable`，不依赖飞书连接器或 Skill Catalog。
- `apps/gateway/internal/httpapi/simple_chat.go`：每轮根据会话 Agent 快照解析 Wiki 节点最新版本；与本轮手动引用合并去重并共同执行 20 个文件、100 MiB 限制。
- `apps/agent-runtime/`：复用现有 `/workspace/wiki` 安全物化、完整性校验和每轮清理；Cocola Wiki 不作为飞书远程资源注入。
- `apps/web/components/agents/agent-capabilities-editor.tsx`：增加 Cocola Wiki 文件选择器，复用 `/api/wiki/tree`，只允许选择个人 Wiki 中的文件节点。
- `packages/proto/`：Agent Knowledge 来源增加 `node_id` 并重新生成 Go/Python 代码。
- 关键取舍：Agent 快照固定 Wiki 节点 ID，每轮读取当前最新不可变版本；删除的 Agent Wiki 来源当轮忽略且不复用 Sandbox 旧文件，存储故障则失败关闭。
