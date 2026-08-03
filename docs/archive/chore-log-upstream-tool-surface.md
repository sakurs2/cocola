# chore: 安全记录上游模型工具面

- 变更时间：2026-08-04 01:50 (+08:00)

## 变更理由

通过 Anthropic Messages 兼容模型运行 Claude Code Plan Mode 时，模型可能调用被禁用的
`Write`，或未调用原生 `ExitPlanMode`。现有日志只能确认上游请求返回 HTTP 200，无法判断
这些工具是否实际出现在最终发往模型的 `tools` 列表中，因此不能准确区分模型工具选择问题与
Cocola / Claude Agent SDK 的工具暴露问题。

## 变更内容

- `apps/llm-gateway/cocola_llm_gateway/upstream/anthropic.py`：在流式与非流式 Anthropic
  payload 发出前记录工具名摘要；限制最多 128 项，并将非法名称替换为固定标记。
- `apps/llm-gateway/tests/test_tool_use_passthrough.py`：覆盖工具名记录、敏感字段隔离、非法名称
  脱敏和超量截断。
- 关键取舍：日志不记录消息、提示词、工具描述、输入 schema、工具参数、请求头或凭据，只记录
  最终上游 payload 中的工具名称与有界计数元数据。
