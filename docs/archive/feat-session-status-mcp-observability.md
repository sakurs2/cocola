# feat: 在 Agent 会话中展示 MCP 加载状态

- 变更时间：2026-07-10 22:58 (+08:00)

## 变更理由

MCP 配置改为由首次真实 Agent 会话自然验证后，用户无法直接判断某个 MCP 是否在当前运行环境中加载成功。依赖 Agent 文本回答来推断工具可用性既不准确，也会诱导把运行状态注入 system context；为配置测试额外申请 sandbox 又会显著扩大链路和资源成本。

## 变更内容

- `deploy/sandbox-runtime/shim/agent_shim.py`：复用执行当前对话的 `ClaudeSDKClient`，在模型请求开始后异步读取 MCP 状态；只对 `pending` 状态进行最多 8 秒的有界查询，并输出脱敏的 `environment_status` 完整快照。
- `apps/agent-runtime/cocola_agent_runtime/shim_provider.py`：将 shim 环境快照转换为 Gateway 可透传的 Agent event，不把状态事件误判为模型内容。
- `apps/web/app/runtime-provider.tsx`：校验并保存当前 turn 的环境状态，不将其混入 assistant-ui 消息历史。
- `apps/web/components/assistant-ui/session-status-panel.tsx`、`apps/web/app/page.tsx`：增加与现有消息 UI 一致的响应式 Session Status Context Dock，展示 MCP 的 Connecting、Connected、Failed、Needs auth、Unavailable 和 Timed out 状态；与 artifact 复用右侧空间，TopBar 约束在聊天主列内，避免覆盖 Dock Header。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：在请求开始时先根据有效配置发送 MCP pending 名单，再由 sandbox 中 SDK 的真实状态覆盖；新旧 runtime 镜像滚动期间不再把“未收到状态”误报为“未启用 MCP”。
- `apps/agent-runtime/tests`：覆盖成功、失败、超时、错误脱敏及 Agent event 映射。
- `docs/frontend-tech-stack.md`：记录运行时状态、UI Dock、无额外 sandbox 和非持续健康监控的架构边界。

## 关键取舍

- 不新增 Agent/Sandbox Proto、数据库结构、服务或前端依赖。
- 不修改 system prompt，不让模型承担环境探测职责。
- 状态采集与回答并行，不阻塞模型首 token；终态后不再轮询。
- 状态只代表本轮 Agent 初始化时的加载结果，不承诺 MCP 的持续健康。
