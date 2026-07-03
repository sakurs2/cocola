# feat: temporary multi-model config

- 变更时间：2026-07-03 10:41 (+08:00)

## 变更理由

用户希望 cocola 先支持临时多模型配置与对话界面模型选择，最终再演进到管理员动态配置。当前 runtime 仍是 Claude Code CLI，因此 v1 需要在不引入 one-api、不改变 agent 工具体系的前提下，把用户可见模型 alias、展示信息、runtime 兼容性和上游连接配置统一到本地 JSON 文件中。

## 变更内容

- `deploy/llm-config.example.json`：更新为临时多模型配置示例，区分 `providers` 与 `routes`，增加 `runtime`、`label`、`icon`、`enabled`、`visible`。
- `apps/web/app/api/models/route.ts`：新增 Web 侧模型列表 API，读取 `deploy/llm-config.json` 并按当前 runtime 过滤可见模型。
- `apps/web/app/runtime-provider.tsx`、`apps/web/components/assistant-ui/thread.tsx`：接入模型列表、composer 内模型选择器、chat 请求 `model_alias`，并在 assistant 消息 metadata 中保存和回放模型展示信息。
- `apps/gateway/internal/httpapi`、`apps/gateway/internal/agent`、`apps/gateway/internal/convo`：接收模型字段、转发 alias 到 agent-runtime、持久化 assistant 消息 metadata。
- `apps/agent-runtime/cocola_agent_runtime`：从 gRPC metadata 读取并校验模型 alias，将其注入 Claude Code sandbox 执行环境。
- `db/migrations/00004_message_metadata.sql`：为消息表增加 `metadata_json`，用于保存模型展示信息等非正文元数据。
- 关键取舍：v1 通过 gRPC metadata 传递 `model_alias`，避免当前缺少 proto 生成工具时修改 proto/generated 代码；未来接入 Codex CLI 时应新增 runtime adapter，而不是复用 Claude Code/Anthropic 协议。
