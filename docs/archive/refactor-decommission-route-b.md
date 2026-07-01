# refactor: 下线 Route B，agent-runtime 收敛为 Route A 单路径

## 背景

Route A（ADR-0009，brain-in-sandbox）已在 opensandbox 后端全栈端到端验证成功
（Web 对话、原生 Bash 工具、真实 `/v1/messages` 出网计费均正常）。此前 Route B
（中心化 SDK 路径，`ClaudeAgentSDKProvider` 在 agent-runtime 进程内 spawn claude
CLI）保留为最小 fallback；本次将其正式下线，agent-runtime 收敛为单一真实路径。

此前的保守 Plan `docs/plan/hardening-route-b-fallback-cleanup.md`（只删 MCP 缝合层、
保留 provider）已被本次执行取代，其文档头已加状态说明。

## 改动

### 删除实现与依赖它的测试/脚本
- `apps/agent-runtime/cocola_agent_runtime/claude_sdk_provider.py`（309 行，Route B 唯一实现）。
- `apps/agent-runtime/tests/test_claude_sdk_provider.py`（纯 provider 映射单测，随代码走）。
- `scripts/llm-m3-e2e.py`、`scripts/llm-m4-e2e.py`（M3/M4 e2e，唯一入口是被删的 provider；
  未接入任何 Makefile/CI target，仅 ADR-0004/0005 引用为历史验收记录）。
- `apps/llm-gateway/tests/test_token_passthrough_e2e.py`（拿被删 provider 当测试载体的
  gateway 契约 e2e）。**其 token 透传 / 401 / 429 / 计费归属这条契约已由 provider 无关的
  `test_server_auth_quota.py` 完整覆盖，删除不丢覆盖率。**

### 收敛 provider 选择
- `apps/agent-runtime/cocola_agent_runtime/__main__.py`：`_build_provider` 塌缩为两层——
  有 sandbox executor（`COCOLA_SANDBOX_ADDR`）→ `InSandboxShimProvider`（Route A）；
  否则 → `EchoProvider`（零配置、无模型调用，仍走通 gRPC 契约）。删除
  `COCOLA_LLM_BASE_URL` / `ClaudeAgentSDKProvider` 分支；不再读取 `COCOLA_AGENT_ROUTE`
  开关（executor 是否存在即决定路径）；更新模块 docstring。

### 清理配置开关
- `deploy/docker-compose/docker-compose.full.yml`：删 `COCOLA_AGENT_ROUTE` env 行，改注释
  （Route A 由 `COCOLA_SANDBOX_ADDR` 存在即激活）。
- `scripts/run-stack.sh`：去掉 `COCOLA_AGENT_ROUTE` 透传与 `COCOLA_LLM_BASE_URL` 的
  agent-runtime 注入（provider 不再读取）；`--with-llm` 改导出 `COCOLA_SANDBOX_LLM_BASE_URL`
  （沙箱内 CLI 回连 gateway 的通道）；更新头部注释与端口约定说明。
- `scripts/mvp-local-e2e.sh`：EchoProvider 触发条件从「`COCOLA_LLM_BASE_URL` unset」改为
  「`COCOLA_SANDBOX_ADDR` unset」。

### 注释收敛（命名去 Route B 化）
- `server.py` / `agent_provider.py` / `skill_loader.py` / `shim_provider.py`
  / `deploy/sandbox-runtime/shim/agent_shim.py` / `llm-gateway auth/identity.py`：
  对 `ClaudeAgentSDKProvider` 的命名引用改为中性表述（Route A / 通用 provider / 沙箱 shim）。

### 文档
- `docs/adr/0009-agent-runtime-in-sandbox.md`：新增「实现进展（2026-07-02）下线 Route B」，
  并更新此前「保留为最小 fallback」「作为独立后续任务评估」的表述为已落地。
- `docs/adr/0004`、`docs/adr/0005`：把「由 e2e 脚本驱动」的表述改为「由
  `test_server_http.py` / `test_server_auth_quota.py` 等 provider 无关的 gateway 契约测试覆盖」。
- `docs/plan/hardening-route-b-fallback-cleanup.md`：加状态头，标注被本次执行取代。
- `README.md`：「接入真实 LLM 链路」段改写为 Route A 说明（`COCOLA_SANDBOX_ADDR` +
  4 个 `COCOLA_SANDBOX_*` 凭证注入），并保留一条 Route B 已下线的历史注记。

## 回滚性

Route B 的一键回滚（`unset COCOLA_AGENT_ROUTE`）随本次下线不再适用；回滚需 revert 本次提交。
这是 ADR-0009 采纳 Route A 为唯一路径后的预期收敛。

## 验收

- agent-runtime：`ruff check .` 全过；`pytest` **69 passed, 2 skipped**。
- llm-gateway：`pytest`（`--extra dev`）**110 passed, 3 skipped**。
- 代码内零 `claude_sdk_provider` / `ClaudeAgentSDKProvider` 残引用（仅保留 ADR / changelog /
  一处 `__main__.py` 历史注释的说明性提及）。
