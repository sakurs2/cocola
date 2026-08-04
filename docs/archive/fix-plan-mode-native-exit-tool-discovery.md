# fix: Restore native ExitPlanMode discovery in SDK Plan Mode

- 变更时间：2026-08-04 12:35 (+08:00)

## 变更理由

使用 Claude Agent SDK 的 headless Plan Mode 时，Claude Code 将 `ExitPlanMode` 注册为需要宿主权限处理能力的 deferred built-in。Cocola 未配置 SDK permission callback，也未在自定义 `ANTHROPIC_BASE_URL` 场景启用 ToolSearch，导致上游请求既没有 `ExitPlanMode`，也没有加载它的通道。模型在计划落盘后只能猜测工具入口，因而错误调用 Skill 并最终触发 `PLAN_OUTPUT_INVALID`。

## 变更内容

- `deploy/sandbox-runtime/shim/agent_shim.py`：仅在 Plan Mode 启用 `ENABLE_TOOL_SEARCH`，配置不授予任何额外权限的 SDK permission callback，使 Claude Code 注册原生交互工具；通过原生 plan workflow 参数明确使用 `ToolSearch(select:ExitPlanMode)` 后调用 `ExitPlanMode`。
- `deploy/sandbox-runtime/shim/agent_shim.py`：记录计划是否已成功持久化；落盘后的 Stop Hook 只要求加载并调用原生 `ExitPlanMode`，不再要求重复提交计划。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：同步 Plan Mode 指令，禁止把 `ExitPlanMode` 当作 Skill 查找。
- `apps/agent-runtime/tests/test_agent_shim_plan_control.py`：覆盖 ToolSearch、原生 workflow 参数、permission callback、持久化后的 Stop Hook，以及 Execute Mode 不受影响的契约。
- 关键取舍：继续使用 Claude Code 原生 Plan Mode、原生计划文件和原生 `ExitPlanMode`；permission callback 始终拒绝交互授权，项目写工具仍由 disallowed tools 与 PreToolUse Hook 双重阻断。
