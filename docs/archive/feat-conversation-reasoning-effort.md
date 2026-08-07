# feat: 对话框支持模型推理等级

- 变更时间：2026-08-07 11:58 (+08:00)

## 变更理由

对话框只能选择模型，用户无法按任务复杂度选择模型原生的推理等级，也无法在 Plan 执行、问题回答等后续链路中保持同一等级。不同模型协议支持的原生档位并不一致，因此需要由模型路由显式声明能力，并在全链路校验和持久化，避免把不受支持的参数发送给上游。

## 变更内容

- `apps/web/components/assistant-ui/thread.tsx`、`apps/web/app/runtime-provider.tsx`、`apps/web/lib/reasoning-effort.mjs`：在现有 HeroUI 模型菜单顶部加入 Reasoning 子菜单，提供 Auto / Fast / Deep / Max 产品档位；按会话保存选择，将其映射为模型原生 effort，并随聊天请求和消息快照发送。
- `apps/web/app/admin/models/page.tsx`、`apps/admin-api/`：支持管理员为每条模型路由声明可用的原生推理档位，并在公开模型列表中返回能力集合。
- `apps/gateway/`、`packages/proto/`、`apps/agent-runtime/`：新增 `reasoning_effort` 字段，贯通聊天、gRPC、Plan 执行和问题回答链路。
- `apps/llm-gateway/`：保留 Anthropic `thinking` / `output_config` 参数，并按模型路由校验 Anthropic Messages 与 OpenAI Responses 的 effort。
- `deploy/sandbox-runtime/shim/`：分别把推理等级映射到 Claude Agent SDK 的 `effort` 和 Codex SDK 的 `modelReasoningEffort`。
- `db/migrations/00057_reasoning_effort.sql`：增加模型路由能力数组，以及 Run / Plan / Question 的推理等级快照字段和数据库约束。
- 相关 Go、Python、Node 测试：覆盖预设映射、能力校验、参数透传、快照持久化及 Plan / Question 继承。
- 关键取舍：Auto 使用模型默认值且显式保存空字符串；路由未声明能力时仅开放 Auto，不对未知模型猜测支持范围。
