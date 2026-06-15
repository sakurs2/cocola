# refactor(agent-runtime): 删除 Route-B MCP 转发缝合层

落地 ADR-0009 §1「Native tools are now safe — delete the MCP forwarding seam」。
Route A 下大脑已在用户沙箱内、原生 Bash/Read/Write 天然隔离，宿主进程「把工具
经 in-process MCP 转发进他人沙箱」的缝合层成为死代码与多余攻击面，本次删除。

## 设计抉择

外科式只删缝合层，**保留** `ClaudeAgentSDKProvider` 作为最小 fallback：

- ADR-0009 §1 明文只点名删除 MCP 缝合层（`sandbox_tools.py` + provider 里的 MCP
  路径），未要求删除 provider 本身。
- provider 仍是独立承重件：llm-gateway JWT 透传契约测试、`llm-m3/m4-e2e.py`、
  零沙箱 dev boot 回落都依赖它。
- 真正的攻击面就是缝合层；删它即达成安全目标，且保住 `unset COCOLA_AGENT_ROUTE`
  一键回滚。收敛 Route A 单路径作为独立后续任务评估，不在本次范围。

## 改动

- 删除 `apps/agent-runtime/cocola_agent_runtime/sandbox_tools.py`（174 行）。
- 删除 `apps/agent-runtime/tests/test_sandbox_tools.py`（149 行）。
- `claude_sdk_provider.py`：移除 `SandboxExecutor` import、`__init__` 的
  `executor` 形参与 `self._executor`；`_build_options()` 移除挂载
  `cocola_sandbox` MCP server + `allowed_tools` 的分支，仅留 model/system_prompt/
  max_turns/env。
- `__main__.py`：Route-B 分支改 `ClaudeAgentSDKProvider(cfg)`、去 `sandbox_tools=`
  日志字段；`_build_executor()` 保留（仍供 Route A shim）、注释更正为 Route A 语义。
- `docs/adr/0009-agent-runtime-in-sandbox.md`：勾掉对应 follow-up 与「尚未删除」
  状态，新增「实现进展（2026-06-15）」小节，fallback 表述更正。

## 验收

- `apps/agent-runtime`：`uv run ruff check` / `ruff format --check` 全过；
  `uv run pytest` → 48 passed, 2 skipped。
- 代码内零 `sandbox_tools` / `build_sandbox_mcp_server` 残引用。
- `disallowed_tools` 按 §1 判定 moot，确认从未实现、本次不引入。

## 不做

- 不删 `ClaudeAgentSDKProvider`、不动 `COCOLA_AGENT_ROUTE` 开关、不改
  docker-compose / run-stack（属「Route A 单路径收敛」后续独立任务）。

## 回滚

- 单 commit，可整体 `git revert`；删缝合层不影响 Route A 主链路。
