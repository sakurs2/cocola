# feat: Agent 长任务改用步骤级超时

- 变更时间：2026-07-17 12:18 (+08:00)

## 变更理由

复杂 Agent 任务可能包含上百个、每个持续数分钟的步骤。原有 Gateway 和
Agent Runtime 都会在 3600 秒后取消整个 Run，默认 30 turns 也不足以完成此类任务。
用户期望不限制整个 Agent Run 的总耗时，只限制单次模型请求和单个工具步骤，同时
保证超时后 Session Workspace 仍可继续使用。

## 变更内容

- `apps/gateway`：移除整个 Agent Run 的 wall-clock deadline；新增事件驱动的工具步骤
  watchdog，`tool_use` 开始计时、`tool_result` 停止计时，超时以 `STEP_TIMEOUT` 结束
  当前 Run；从 Postgres 动态读取 max turns 和工具步骤超时，并限制客户端只能降低
  max turns。
- `apps/agent-runtime`、`apps/sandbox-manager`、`packages/proto`：Agent 主 Exec 使用
  `timeout_secs=-1` 表示无总 deadline，仍响应调用方取消；普通 Exec 的 provider 默认
  超时保持不变。
- `apps/admin-api`、`apps/web/app/admin/settings`：新增 `execution.agent_max_turns` 和
  `execution.tool_step_timeout_secs` 设置，数据库覆盖从下一个新 Run 起热生效。
- `apps/llm-gateway`：单次模型请求默认超时从 300 秒调整为 600 秒。
- `apps/cli`、`scripts/run-stack.sh`、`.env.example`：生成并注入长任务默认配置；Sandbox
  用户 token 默认有效期调整为 7 天。模型请求超时和 token TTL 仍需重启对应进程。
- `docs/configuration.md`：记录配置优先级、热加载边界、超时语义和旧总 Run 超时变量的
  删除。
- 关键取舍：不新增任务队列、后台轮询、自动续跑或新的状态机；步骤超时只终止当前
  Run，Session PVC、Workspace 和 Runtime 会话状态继续保留。
