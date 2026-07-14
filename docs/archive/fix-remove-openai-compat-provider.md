# fix: 删除不完整的 OpenAI Chat Completions Provider

- 变更时间：2026-07-14 01:12 (+08:00)

## 变更理由

Codex Runtime 接入后，`openai_compat` Provider 被发布为 `anthropic-messages`
兼容模型，Admin 页面因此允许 Claude Code 选择 Chat Completions 上游。但该 adapter
只转发纯文本消息，没有无损转换 Claude Code 依赖的 tools、tool choice、tool use、
tool result 和 Anthropic content blocks。模型在询问 MCP 等工具相关问题时可能把内部
`tool_condition` 描述当成普通回答输出。

Cocola 的核心是完整 Agent Runtime，而不是通用聊天协议聚合。继续保留一个只能处理
文本、却被标记为 Claude Code 兼容的 adapter 会制造不可靠的中间状态。

## 变更内容

- `apps/llm-gateway`：删除 `OpenAICompatUpstream` 及其配置分支和测试引用。
- `apps/admin-api`：Provider 类型只接受 `anthropic` 和 `openai_responses`，并增加旧
  `openai_compat` 类型被拒绝的回归测试。
- `apps/web`：Admin Models 页面移除 OpenAI Chat Completions 选项、兼容性标签和
  `/chat/completions` 端点展示。
- `db/migrations/00036_remove_openai_compat.sql`：升级时拒绝残留旧 Provider，并用
  数据库约束把允许类型收口到 Anthropic Messages 和 OpenAI Responses。
- `README.md`、`docs/adr/0004-*`、`docs/adr/0022-*`：明确 Claude Code 与 Codex
  分别使用原生 Messages 和 Responses 协议，不实现有损 Chat Completions 转换。
- 关键取舍：不在 Gateway 中补一套复杂的双向工具协议转换；供应商只有实现 Runtime
  所需原生协议后才作为可配置 Provider。
