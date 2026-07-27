# feat: 用户级 AGENTS.md

- 变更时间：2026-07-27 17:14 (+08:00)

## 变更理由

用户需要为自己的所有 Agent 对话设置持久规则，例如回复语言、代码风格和工作方式。规则应由用户本人维护，每轮对话自动生效，同时不能覆盖管理员策略、安全约束或仓库内更具体的规则。

## 变更内容

- `db/migrations/00050_user_agent_instructions.sql`：新增按用户隔离的 AGENTS.md 存储，保存正文、版本和更新时间。
- `apps/admin-api`：新增用户本人可读写的 `/me/agent-instructions` 接口，并把用户规则并入 runtime 的有效提示词配置；正文限制为 32 KB。
- `apps/agent-runtime`：每轮获取最新用户规则，并作为低于管理员和平台策略的持久用户指令注入 system prompt；发生冲突时只忽略用户规则中的冲突部分。
- `apps/web`：在 Profile 增加简单的 AGENTS.md 编辑器，支持显式保存和清空，不引入模板、历史版本或多规则集。
- 测试覆盖用户隔离存储、HTTP 读写和大小限制、管理员与用户规则的合并顺序，以及最近有效配置缓存的容量上限。

关键取舍：复用现有 admin-api effective prompt 链路，不修改 Gateway 和 gRPC，也不向沙箱重复同步文件；`AGENTS.md` 是用户管理持久规则的配置格式，运行时统一通过 system prompt 对当前 Agent 生效。
