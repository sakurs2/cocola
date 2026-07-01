# run-stack.sh 默认路由切换为 Route A

## 背景

`COCOLA_AGENT_ROUTE` 在两个本地启动入口的默认值不一致:

- `deploy/docker-compose/docker-compose.full.yml` 默认 `A`(brain-in-sandbox,ADR-0009);
- `scripts/run-stack.sh` 默认空 → 落到 `_build_provider` 的中间分支,走 Route B
  (`ClaudeAgentSDKProvider`,中心化 SDK)。

结果:经 `run-stack.sh` / `make up-all` 起的本地栈实际跑的是 Route B,与
ADR-0009 已采纳 Route A 为默认形态的方向相悖,也容易让人误以为"最近这次对话测试
= Route A"。日志实证:`.run-logs/agent-runtime.log` 首行为 `using
ClaudeAgentSDKProvider` + `COCOLA_SANDBOX_ADDR unset` + `has_sandbox: false`。

## 改动

`scripts/run-stack.sh`:

- agent-runtime 启动块的 `COCOLA_AGENT_ROUTE="${COCOLA_AGENT_ROUTE:-}"` 改为
  `"${COCOLA_AGENT_ROUTE:-A}"`,与 compose.full 对齐,默认走 Route A。
- 更新头部 Design notes:说明默认 Route A、需要 `COCOLA_SANDBOX_ADDR` 指向一个
  sandbox-manager 才是"真 Route A"运行;无 executor 时按 `_build_provider` 逻辑
  自动降级到 Route B;要强制旧的中心化 SDK 路径设 `COCOLA_AGENT_ROUTE=B`。

## 兼容性 / 降级

- `run-stack.sh` 本身不启动 sandbox-manager(其构建是容器化的)。因此不导出
  `COCOLA_SANDBOX_ADDR` 时,Route A 会在 agent-runtime 侧自动回退到 Route B——
  行为不比改动前差,只是默认意图从"永远 B"变为"能 A 则 A"。
- 要跑真正的 Route A 活栈,用 `docker-compose.full`(默认 route=A 且拉起
  sandbox-manager),或给 `run-stack.sh` 显式导出 `COCOLA_SANDBOX_ADDR`。

## 验证

- `apps/agent-runtime` 全量单测:79 passed, 2 skipped;ruff All checks passed。
  其中 Route A 关键用例:
  - `test_session_id_is_reused_as_resume_next_turn`(多轮 resume:第二轮注入
    `resume=<claude_session_id>`)
  - `test_maps_tool_use_turn_and_reassembles_split_line`(shim NDJSON 事件映射)
  - `test_requires_bound_sandbox`(无 sandbox 时拒跑 Route A)
- `bash -n scripts/run-stack.sh` 语法 OK。

## 活栈端到端复核(需在 Linux+Docker / 本机 Docker 上人工跑一次)

```
COCOLA_AGENT_ROUTE=A docker compose -f deploy/docker-compose/docker-compose.full.yml up
# 发一轮对话后,agent-runtime 日志应出现:
#   using InSandboxShimProvider (Route A: brain in sandbox)
#   has_sandbox: true
# SSE 流应带 event: sandbox(含 sandbox_id)。
```
