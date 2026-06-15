# Plan: 删除 Route-B MCP 转发缝合层（收敛攻击面）

- 关联：ADR-0009 §1「Native tools are now safe — delete the MCP forwarding seam」、
  ADR-0009 Follow-ups「delete `sandbox_tools.py` and the Route-B MCP path in
  `claude_sdk_provider.py`」、ADR-0009「实现进展」中仍未做项第 2 条。
- 性质：纯 Python，无运行时依赖变更，本机（macOS）可完整完成与测试，风险低。
- 目标：移除 Route B 把宿主 Agent 的 bash/read/write 经 in-process MCP **转发进
  沙箱**的缝合层。这是 Route B 时代为「中心化大脑 + 远端双手」打的补丁；Route A
  下大脑已在用户沙箱内、原生工具天然隔离，该缝合层成为死代码且是多余的攻击面
  （宿主进程持有指向某沙箱文件系统的工具句柄）。

## 现状（blast radius，已勘定）

- `apps/agent-runtime/cocola_agent_runtime/sandbox_tools.py`（174 行）—— 缝合层本体：
  `sandbox_tool_defs` / `tool_names` / `build_sandbox_mcp_server` / `SERVER_NAME`。
- `apps/agent-runtime/cocola_agent_runtime/claude_sdk_provider.py`：
  - `__init__(... executor=None)` 参数与 `self._executor`。
  - `_build_options()` 内 `if self._executor is not None and options.sandbox_id:`
    分支（import sandbox_tools、挂载 `mcp_servers` + `allowed_tools`）。
- `apps/agent-runtime/cocola_agent_runtime/__main__.py`：
  - `_build_provider()` Route-B 分支 `ClaudeAgentSDKProvider(cfg, executor=executor)`
    与日志字段 `sandbox_tools=executor is not None`。
- `apps/agent-runtime/tests/test_sandbox_tools.py`（149 行）—— 整文件随缝合层删除。
- ADR-0009：line 142-143 follow-up、line 167「尚未删除」状态需更新。

## 设计抉择（关键）

任务允许「收敛 Route A 单路径」或「保留最小 fallback」。**本 Plan 采用后者：
外科式只删缝合层，保留 `ClaudeAgentSDKProvider` 作为最小 fallback。** 依据：

1. **ADR 授权边界**：ADR-0009 §1 明文只要求删除 MCP 缝合层（`sandbox_tools.py`
   + provider 里的 MCP 路径），**未**要求删除 `ClaudeAgentSDKProvider` 本身。
2. **该 provider 仍是独立承重件**，与缝合层无关：
   - llm-gateway 契约测试 `apps/llm-gateway/tests/test_token_passthrough_e2e.py`
     用它断言「网关校验的 cocola JWT == SDK 透传的 ANTHROPIC_AUTH_TOKEN」。
   - e2e 脚本 `scripts/llm-m3-e2e.py` / `llm-m4-e2e.py` 依赖它。
   - 零沙箱 dev boot（`COCOLA_SANDBOX_ADDR` 未设 → 无 executor → Route A 不可用）
     需回落到它。
3. **最小攻击面 + 不重复造轮子**：真正的攻击面是「宿主进程转发工具进他人容器」
   这一缝合层，删它即可；连带删 provider/开关/compose/run-stack 是超出 ADR
   授权的大改动，且会破坏契约测试、丢失一键回滚（`unset COCOLA_AGENT_ROUTE`）。

> 收敛为 Route A 单路径（删除 provider + `COCOLA_AGENT_ROUTE` 开关 + 改
> __main__/compose/run-stack）作为**独立的后续任务**评估，不在本次范围。

## 改动清单

1. **删除** `apps/agent-runtime/cocola_agent_runtime/sandbox_tools.py`。
2. **删除** `apps/agent-runtime/tests/test_sandbox_tools.py`（其中
   `test_provider_mounts_*` 三个 provider 挂载断言随缝合层一并失效）。
3. **`claude_sdk_provider.py`** 去缝合：
   - `__init__` 移除 `executor` 形参与 `self._executor`、相关注释。
   - `_build_options()` 移除 import sandbox_tools 与 `mcp_servers`/`allowed_tools`
     挂载分支；options 只保留 model/system_prompt/max_turns/env。
   - 顶部 docstring/注释中关于 sandbox_tools 的描述清理。
4. **`__main__.py`**：
   - Route-B 分支改为 `ClaudeAgentSDKProvider(cfg)`（不再传 executor）。
   - 移除日志字段 `sandbox_tools=...`。
   - `_build_executor()` 保留不动（仍供 Route A 的 `InSandboxShimProvider` 用）。
5. **ADR-0009** 状态对齐：
   - line 142-143 follow-up「delete `sandbox_tools.py` ...」加删除线 + ✅。
   - line 167「`sandbox_tools.py` 与 Route-B MCP 路径尚未删除」改为已完成。
   - §1 / line 157 fallback 表述微调：明确「Route B 保留为最小 fallback，但
     MCP 转发缝合层已删除，原生工具不再经宿主转发」。
6. **`docs/archive/`** 新增本次 changelog。

## 验收

- `cd apps/agent-runtime && uv run ruff check . && uv run ruff format --check .`
- `uv run pytest`（agent-runtime 全绿，确认无 sandbox_tools 残引用、provider
  其余用例不破）。
- `apps/llm-gateway` 的 `test_token_passthrough_e2e.py` 仍全绿（证明 provider
  契约未被波及）。
- `grep -rn "sandbox_tools\|build_sandbox_mcp_server" apps/agent-runtime`
  仅余 ADR/archive 文档引用，代码内零残留。

## 回滚

- 单次 commit，可整体 `git revert`。删缝合层不影响 Route A 主链路。

## 不做

- 不删 `ClaudeAgentSDKProvider`、不动 `COCOLA_AGENT_ROUTE` 开关、不改
  docker-compose / run-stack（属「Route A 单路径收敛」后续独立任务）。
- 不实现 `disallowed_tools`（ADR-0009 §1 已判定 moot；grep 确认从未实现）。
