# fix: 预创建 Claude 原生计划目录

- 变更时间：2026-08-04 14:12 (+08:00)

## 变更理由

Qwen 等 Anthropic Messages 兼容模型在 Cocola Plan Mode 中看到 Claude Code 的原生计划文件路径后，可能先尝试通过 Bash 执行 `mkdir -p /home/cocola/.claude/plans`。该命令属于写操作，会被原生 Plan Mode 权限正确拒绝；随后 `cocola_update_plan` 虽然仍能安全创建目录并完成计划落盘，但对话中会出现一次无意义的失败工具调用。

## 变更内容

- `deploy/sandbox-runtime/runtime-entrypoint.sh`：在 Agent 运行前预创建 `/home/cocola/.claude/plans`，固定属主为 `cocola:cocola`、权限为 `0700`，并对持久化 Session 中的符号链接异常 fail closed。
- `apps/agent-runtime/cocola_agent_runtime/server.py`、`deploy/sandbox-runtime/shim/agent_shim.py`：明确禁止模型通过 Bash、mkdir、touch 或普通写工具管理原生计划目录和文件，统一交由 `cocola_update_plan` 处理。
- `apps/agent-runtime/tests/test_agent_shim_plan_control.py`：覆盖 Agent Runtime 与 Shim 两层 Plan 指令契约。
- `scripts/sandbox-runtime-verify.sh`：验证镜像启动后计划目录已存在且所有权、权限正确。
- `deploy/sandbox-runtime/README.md`：记录计划目录的启动时保证。
- 关键取舍：不为 `mkdir` 增加 Plan Mode 特殊放行，继续保持普通写操作的只读安全边界。
