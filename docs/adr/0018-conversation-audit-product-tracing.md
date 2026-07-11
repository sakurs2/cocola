# ADR-0018: Conversation Audit 与产品 Trace

- Status: Accepted
- Date: 2026-07-11
- Deciders: @cocola-maintainers

## Context

原有 `audit_events` 同时记录管理操作、读取请求和聊天行为，而产品排障真正需要的是
一次用户消息触发的完整 Agent 执行。原有 `trace_events` 又是无父子关系的扁平计时
列表，无法表达多轮模型调用、工具调用和环境准备之间的因果关系。

外部 OpenTelemetry 默认关闭且默认只采样 5%，不能承担“每一次对话都可查询”的
产品承诺；强制部署 Collector 与 Tempo 又会增加自托管复杂度。

## Decision

- 一次有效的用户或定时任务消息对应一个 `conversation_run`。它同时是对话审计摘要
  和 Trace 根节点；非对话操作不再写产品审计。
- 子步骤以带 `span_id / parent_span_id` 的 `conversation_trace_spans` 表达，详细 Span
  默认保留 30 天，Run 摘要长期保留。
- Gateway 创建 Run 并聚合 Web、Agent Runtime 和 Sandbox Shim 事件；LLM Gateway
  记录每次模型调用。Agent 与 Sandbox Proto 不增加 Trace 字段。
- 全链路使用 W3C `traceparent`。产品数据 100% 写入 Postgres；现有 OTel 管线保持
  可选和低采样，并复用相同的 Trace 标识。
- 默认只记录安全元数据，不复制 Prompt、回答、reasoning、工具输入输出、Header、Env
  或 Secret URL。

## Consequences

- Admin 可以从 Conversation Audit 进入完整、分层的执行瀑布，而无需部署 Tempo。
- 每次 Run 只有一个 `conversation.run` Root；Request、Environment、Agent、Finalization 是固定一级阶段，Model 与 Tool 是 Agent 子 Span。首次事件、Reasoning 和 Token 时间作为阶段属性记录，不单独创建 Span。
- Environment 以聊天 UI 的 Ready/Degraded 事件为结束边界；MCP/Prompt 配置、SDK 初始化和 MCP 连接属于 `agent.initialize`，该子阶段与 Model/Tool 一同包含在 `agent.execute` 内。
- gRPC 的标准 `traceparent` 继续由 otelgrpc 管理；产品层额外使用内部 `x-cocola-product-traceparent` 传递持久化父 Span，避免未落库的 Transport Span 破坏 Admin Trace 树。该内部 Header 不向模型供应商或 MCP 透传。
- Trace Span 异步写入，不把数据库延迟加入 SSE 热路径；写入失败不会影响回答，但会
  产生 partial trace。
- Admin 与安全操作不再存在独立的产品审计历史，只保留结构化服务日志。
- Gateway 与 LLM Gateway 成为仅有的产品 Trace 数据库写入方，避免所有运行时服务
  都直接耦合 Postgres。
