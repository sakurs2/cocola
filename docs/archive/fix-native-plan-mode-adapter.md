# fix: Restore Claude Code native Plan Mode semantics

- 变更时间：2026-08-03 00:10 (+08:00)

## 变更理由

结构化 Plan 控制协议禁止了原生 Write 和 ExitPlanMode，并要求 Claude 调用自定义 `cocola_submit_plan`。Claude 在实际会话中可能返回普通计划文本而不调用该工具，最终触发 PLAN_OUTPUT_INVALID；同时也阻止了 Claude Code 在 Plan Mode 下维护原生计划文件。

## 变更内容

- `deploy/sandbox-runtime/shim/agent_shim.py`：保留 Claude Code 原生 Plan 权限和计划文件能力，捕获并隐藏 `ExitPlanMode`，将其受限 Markdown 载荷转换为 Cocola `plan_ready`，由 Cocola 负责审批。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：Plan 指令改为遵循原生计划文件与 `ExitPlanMode` 工作流，审批后继续复用同一 Claude Session。
- `apps/agent-runtime/tests/test_agent_shim_plan_control.py`：覆盖原生工具配置、计划捕获、重复捕获、大小限制和提示词契约。
- `apps/agent-runtime/tests/test_server.py`：验证运行时下发原生 Plan Mode 指令。
- 关键取舍：项目文件仍由 Claude 原生 Plan 权限和 Cocola 工作区 revision 校验双重保护；Cocola 仅接管审批 UI 与结构化澄清问题。
