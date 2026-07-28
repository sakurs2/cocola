# fix: 飞书机器人仅发送最终回答

- 变更时间：2026-07-28 10:36 (+08:00)

## 变更理由

飞书机器人此前会把 Agent 的 `text` 事件直接流式发送到外部会话。模型在调用工具前输出的执行说明也属于 `text`，因此用户会看到类似思考过程的中间内容，而不是只看到任务完成后的最终回答。

## 变更内容

- `apps/gateway/internal/channel/feishu/manager.go`：停止向飞书流式转发 Agent 事件；优先使用终态 `result.result`，缺失时仅保留最后一次工具调用之后的文本，并在运行结束后一次性发送。
- `apps/gateway/internal/channel/feishu/manager.go`：恢复已完成快照时同样丢弃最后一次工具调用之前的执行说明，保持重试与首次执行行为一致。
- `apps/gateway/internal/channel/feishu/manager.go`：识别失败、取消和中断的终态快照，避免幂等重试时把快照中的内部错误文本发送到外部会话。
- `apps/gateway/internal/channel/feishu/manager_test.go`：新增终态结果、无终态正文兜底以及快照恢复回归测试，确保 reasoning、工具前说明和草稿不会发送给外部用户。
- 错误提示与待回答问题仍按原有独立消息发送，不暴露内部错误详情或中间正文。
