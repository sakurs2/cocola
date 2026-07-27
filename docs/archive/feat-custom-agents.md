# feat: 自定义 Agent 与独立飞书机器人

- 变更时间：2026-07-27 21:39 (+08:00)

## 变更理由

Cocola 需要支持用户创建有固定身份、指令与模型的自定义 Agent，并让每个 Agent 可选关联一个独立飞书机器人。同时普通 Web 对话和 Project 对话必须继续支持不选择 Agent 的基础模式，避免把两个能力强制绑定或演变为复杂的工作流产品。

原有飞书连接器按用户单例存储模型与机器人配置，无法支持一名用户的多个 Agent/机器人；对话也没有不可变 Agent 快照，Agent 后续编辑会造成历史会话行为漂移。

## 变更内容

- `db/migrations/00051_custom_agents.sql`：新增个人 Agent 表；将飞书连接器和注册 flow 改为 Agent 作用域；为会话增加 Agent 版本、不可变快照和连接器快照引用。测试阶段不兼容旧飞书单例数据，迁移时直接清理。
- `apps/gateway/internal/agentprofile/`、`apps/gateway/internal/httpapi/agents.go`：实现个人 Agent CRUD、输入校验、所有者隔离、乐观版本控制和归档约束。
- `apps/gateway/internal/convo/`、`apps/gateway/internal/chatrun/`、`apps/gateway/internal/httpapi/simple_chat.go`：首次对话原子保存 Agent 快照；后续对话拒绝切换 Agent，并使用快照中的 runtime、模型和指令。会话列表只返回不含 instructions 的轻量 Agent 摘要。
- `apps/gateway/internal/channel/feishu/`：支持同一用户的多个 Agent 机器人；一个 Agent 最多关联一个 bot；入站消息按 connector/Agent 隔离，并将 Agent ID 交给聊天入口。
- `packages/proto/`、`apps/agent-runtime/`：新增 AgentContext，并按“平台指令 → 管理员系统提示 → Agent 指令 → 用户 AGENTS.md → Memory”的顺序组装提示。Lark 凭证由有效连接决定，不再以 Skill 名称或 Plan/Execute 模式作为额外门槛。
- `apps/web/app/runtime-provider.tsx`、`apps/web/components/assistant-ui/thread.tsx`：对话框在模型选择器后增加 Agent 选择器，默认 `None`；选择 Agent 后固定其模型并显示英文锁定提示，首条消息后锁定 Agent。
- `apps/web/app/agents/`、`apps/web/app/api/agents/`、`apps/web/components/agents/`：新增 Agents 列表、创建和单列编辑页，支持内置头像/颜色、指令、固定模型、飞书机器人连接及归档。
- `apps/web/app/connectors/`：移除飞书单例入口与旧代理路由，Connectors 保留 GitHub；飞书配置统一收口到 Agent 详情。
- 测试覆盖 Agent service/HTTP 生命周期、会话快照、飞书多连接器、proto 序列化、运行时提示顺序、Lark 凭证注入以及会话摘要不泄露完整 instructions。

## 关键取舍

- 不引入默认 Agent 实体、Agent 画布、工作流或 Project 级强绑定；`None` 继续代表现有基础对话能力。
- Agent 修改只影响新会话；已有会话固定使用创建时快照。
- Agent 归档前必须显式断开飞书机器人，不做隐式级联删除。
