# fix: Correct the native Plan Mode permission boundary

- 变更时间：2026-08-03 13:17 (+08:00)

## 变更理由

Claude Plan Mode 的 `PreToolUse` Hook 对所有普通工具返回了 `allow`，导致 Hook 在原生权限模式判断前预先批准 `Write`，从而允许规划阶段修改工作区。与此同时，Claude Code 会把计划内容注入 `ExitPlanMode` 的 Hook 输入，而后续 AssistantMessage 仍可能携带空输入；重复处理会把已经捕获的有效计划错误地降级为 `PLAN_OUTPUT_INVALID`。Claude 仅返回普通文本结束规划时，也缺少一次回到原生 `ExitPlanMode` 流程的收敛机会。

## 变更内容

- `deploy/sandbox-runtime/shim/agent_shim.py`：普通工具不再由 Cocola Hook 预授权，交回 Claude Code 原生 Plan 权限判断；保留 Hook 已捕获的原生计划；清理拒绝工具的残留状态；通过一次性 Stop Hook 要求未完成的规划调用 `ExitPlanMode`。
- `apps/agent-runtime/tests/test_agent_shim_plan_control.py`：覆盖工作区写入不被预授权、原生计划输入重复处理、拒绝工具状态释放、Plan Stop Hook 单次收敛，以及 Execute 模式不受影响。
- 关键取舍：Claude Code 仍可在 `/home/cocola/.claude/plans` 下维护受控计划文件，但 Plan Mode 不允许修改项目工作区；计划批准后继续恢复同一 Claude Session 并切换到 Execute。
