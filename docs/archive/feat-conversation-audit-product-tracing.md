# feat: Conversation Audit 与详细 Agent Trace

- 变更时间：2026-07-11 16:03 (+08:00)

## 变更理由

原审计日志混合了管理、读取和聊天事件，信息噪音高；原 Trace 是扁平计时列表，无法
查询从 Web 鉴权、环境准备、Agent SDK、模型调用、工具执行到回答持久化的完整流程。
同时，默认关闭且低采样的外部 OpenTelemetry 不能作为每次对话都可查询的产品数据源。

## 变更内容

- `db/migrations/00028_conversation_runs.sql`：新增 Agent Run 与层级 Span 数据模型，只迁移历史 `chat.send` 审计并清理其他审计类型。
- Gateway、Agent Runtime、Sandbox Shim、LLM Gateway：通过 W3C TraceContext 串联执行阶段，采集 TTFT、模型 Token、工具调用和环境准备耗时；不修改 Agent/Sandbox Proto。
- Agent Runtime：按 Claude SDK 要求使用换行分隔的 `Header-Name: value` 格式注入 `traceparent`，避免 JSON 被误解析为非法 Header 名称。
- Admin API/Web：新增 Conversation Run 查询接口，将 Audit 页面收敛为对话执行列表，并将 Trace Detail 重做为分组执行瀑布与安全属性 inspector。
- Trace 层级：每次 Run 仅有一个 `conversation.run` Root，Request、Environment、Agent、Finalization 是真实一级 Span，Model 与 Tool 挂在 Agent 下；首次事件、Reasoning、Token 和 Tool 时间改为 Agent Metadata，避免重复 Span。
- Trace Detail：按真实 `parent_span_id` 渲染左侧 Trace 树，右侧使用 Run/Metadata 诊断工作台；Conversation Audit 日期范围改为浅色 Radix Popover 日历和常用范围快捷项。
- Trace 传播：使用独立的内部 Product TraceContext 避免 otelgrpc Transport Span 覆盖产品父节点，并在跨服务子 Span 到达前写入 Running 阶段父节点；Trace UI 移除左右两侧的装饰性时间轨道。
- Trace 计时：一级阶段改为 Request → Environment → Agent → Finalization 的连续非重叠区间；Environment 在对话 UI 的 Ready/Degraded 事件结束，流式 Artifact 注册归入 Agent，阶段总和不得超过 Run 总耗时。
- Environment 明细：只包含 Gateway→Runtime dispatch、Sandbox、Checkpoint、Attachments 和 Skills，与用户看到的 Environment 节点保持一致。
- Agent 明细：新增 `agent.initialize` 子阶段承载 MCP 配置、Prompt 配置、SDK 初始化与 MCP 连接等待；停止持久化横跨两个阶段的 `agent.sdk_session`，Model 与 Tool 继续挂在 `agent.execute` 下。
- 旧审计清理：删除 Admin HTTP/登录审计 writer、业务层空审计调用、旧 Store 读写契约和未使用的 Gateway audit writer；历史表仅由迁移读取，兼容接口直接映射新 Run/Span 数据。
- Trace Span 异步写入并默认保留 30 天；队列满或写入失败时 Run 降级为 partial，失联运行自动标记 interrupted，摘要长期保留。
- 关键取舍：Postgres 是 100% 产品 Trace 事实源；OpenTelemetry 保持可选导出和原有采样，不强制部署 Collector/Tempo。
