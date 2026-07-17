# fix: 保留 Anthropic thinking 并稳定 Sandbox 工作目录

- 变更时间：2026-07-16 23:25 (+08:00)

## 变更理由

Agent 对话在启用 thinking 的 Anthropic 兼容模型上进行后续轮次时，上游返回
`content[].thinking must be passed back`。协议转换存在两个问题：非流式响应聚合只处理
文本和工具参数，丢失了 `thinking_delta` 与 `signature_delta`；同时 streaming 和
non-streaming 响应都复用固定的 `msg_cocola`，导致连续 tool-use 子请求的 assistant
消息边界冲突。Session Sandbox 恢复后还可能继承已失效的容器工作目录，导致 outputs
快照出现 `getcwd` 和 `FileNotFoundError`。

## 变更内容

- `apps/llm-gateway/cocola_llm_gateway/anthropic_codec.py`：在非流式响应聚合中保留 thinking 与 signature 增量，并为每次 Anthropic 响应生成唯一 message ID。
- `apps/llm-gateway/cocola_llm_gateway/upstream/anthropic.py`：将非流式上游 thinking block 重放为标准 thinking/signature 事件。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：从绝对 `/workspace/outputs` 生成快照，并显式使用 `/workspace` 作为执行目录。
- `apps/agent-runtime/cocola_agent_runtime/shim_provider.py`：每轮 Sandbox shim 执行前显式进入 `/workspace`。
- `apps/llm-gateway/tests/`、`apps/agent-runtime/tests/`：新增 thinking round-trip、响应 ID 唯一性与稳定工作目录回归覆盖。
- 关键取舍：不增加兼容状态、重试或后台任务，只修复既有协议转换与执行路径。
