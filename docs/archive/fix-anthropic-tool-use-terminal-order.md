# fix: 兼容 DeepSeek Anthropic tool_use 末尾约束

- 变更时间：2026-07-03 21:09 (+08:00)

## 变更理由

对话中 agent 使用工具后，上游 DeepSeek Anthropic-compatible endpoint 返回 400：
`tool_use ids were found without tool_result blocks immediately after`。日志显示
`tool_result` 实际存在于下一条 user 消息开头，但 assistant 消息内部是
`thinking -> tool_use -> thinking -> text`。DeepSeek 对 tool transcript 更严格：
assistant 消息一旦包含 `tool_use`，后续 block 不能再出现 thinking/text，否则会认为
tool_result 没有紧跟 tool_use。

## 变更内容

- apps/llm-gateway/cocola_llm_gateway/upstream/anthropic.py：在发往上游前，将 assistant
  消息中的 `tool_use` block 规范到消息末尾，并继续保持下一条 user 消息以对应
  `tool_result` 开头；校验逻辑也补充检测非末尾 `tool_use`。
- apps/llm-gateway/tests/test_tool_use_passthrough.py：新增回归用例覆盖
  `thinking/tool_use/thinking/text` 这种 DeepSeek 会拒绝的 transcript。
