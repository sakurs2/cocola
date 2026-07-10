# fix: MCP 删除确认与 Session Status 检查时机

- 变更时间：2026-07-11 00:58 (+08:00)

## 变更理由

Admin MCP 删除仍调用浏览器原生 `confirm`，视觉、键盘焦点和错误处理均与 Admin UI 不一致。Session Status 同时在每个 follow-up 中重复检查；观察到 Agent 首 token 与 MCP 状态同时出现，容易误认为状态轮询阻塞回答。

实际调用顺序中 `client.query()` 先于 `get_mcp_status()`，轮询没有阻塞模型请求；共同等待点是 Claude SDK/CLI 初始化，CLI 需要先装载本轮 MCP 配置。重复为 follow-up 创建可控制的 `ClaudeSDKClient` 没有产品增量。

## 变更内容

- `apps/web/components/admin/admin-ui.tsx`：新增基于 Radix Dialog 的 `AdminConfirmDialog`，复用 Admin sky-glass 主题、焦点锁定和 Escape 行为。
- `apps/web/app/admin/mcps/page.tsx`：MCP 删除改用 `AdminConfirmDialog`，删除浏览器 `window.confirm`。
- `deploy/sandbox-runtime/shim/agent_shim.py`：仅无 `resume` 的会话初始化使用 `ClaudeSDKClient` 异步采集 MCP 状态；follow-up 恢复 one-shot SDK 路径，不再发 `environment_status`。
- `apps/agent-runtime/cocola_agent_runtime/server.py`、`apps/web/app/runtime-provider.tsx`：删除 agent-runtime 每轮预置的 pending 事件；前端仅在会话第一条消息发送时立即进入 Preparing，再由首次 sandbox SDK 真实快照覆盖，避免等待 Exec 流后面板才出现。
- `apps/agent-runtime/tests`：覆盖首轮 query 先于状态读取，以及 resumed turn 不构造状态 client、不发状态事件。
- `docs/frontend-tech-stack.md`：记录 Session Status 每个 Agent 会话只检查一次的边界。

## 关键取舍

- 不缓存或伪造 MCP 健康状态；Session Status 继续表示实际会话初始化结果。
- 不从 follow-up 移除 MCP 配置，避免工具在后续轮次消失；CLI 自身重新装载 MCP 的耗时仍属于 Agent 启动路径。
- 若首轮某个 MCP 连接缓慢，Claude CLI 初始化仍可能延迟首 token；状态轮询只观察该过程，不是延迟来源。
