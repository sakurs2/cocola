# fix: 统一 Plan Mode 的受控计划写入链路

- 变更时间：2026-08-03 15:41 (+08:00)

## 变更理由

Claude Code 运行时接入 Anthropic Messages 兼容模型时，模型可能忽略原生 Plan Mode 提供的计划文件路径，先尝试修改项目文件，再自行编造 `/home/user/.claude/plans/` 下的错误路径。原生权限虽然阻止了副作用，但计划无法写入并调用 `ExitPlanMode`，最终返回 `PLAN_OUTPUT_INVALID`。平台不能依赖具体模型是否完整理解 Claude Code 的内部计划文件约定。

## 变更内容

- `deploy/sandbox-runtime/shim/agent_shim.py`：Plan Mode 禁用普通 `Write`、`Edit` 和 `NotebookEdit`，新增模型无关的 `cocola_update_plan` 控制工具；从 Claude Code 转录中校验本轮精确 `planFilePath`，通过受限目录句柄原子写入原生计划文件，并继续使用 `ExitPlanMode` 完成审批。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：明确统一的计划工具调用顺序，禁止在审批前实施用户请求。
- `apps/agent-runtime/tests/test_agent_shim_plan_control.py`：覆盖工具暴露、项目写入拒绝、可信计划写入、伪造路径、符号链接和并行提交等边界。
- `apps/agent-runtime/tests/test_server.py`：验证对话 Plan Mode 注入统一控制提示。
- 关键取舍：所有模型使用同一条 Plan Mode 链路，不按模型名称或 Provider 类型分支；计划仍落入 Claude Code 原生会话存储，并由原生 `ExitPlanMode` 进入 Cocola 审批流程。
