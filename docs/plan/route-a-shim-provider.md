# Plan: agent-runtime 切到 Route A —— InSandboxShimProvider(步骤 1+2)

- 状态: Approved(待实现)
- 日期: 2026-06-11
- 关联: ADR-0009(运行时进沙箱)、ADR-0010(网关 tool-use 透传)
- 范围: 落地 ADR-0009 的 Route A 切换的前两步(接通 shim provider + 灰度切换),
  Route B(`ClaudeAgentSDKProvider`)保留为 fallback。

## 1. 背景与现状

ADR-0009 决定把 Claude Code 运行时整体放进每个用户的沙箱(Route A),
agent-runtime 退化为控制面路由。代码侧目前:

- 沙箱镜像 + stdio shim 已完成(commit c3ce8e1)。
- agent-runtime 仍是 Route B:`claude_sdk_provider.py`(中心进程 spawn claude)
  + `sandbox_tools.py`(MCP 转发)都还在,Route A 切换未落地。
- ADR-0009 状态仍为 Proposed。

本 Plan 只做"接通 Route A 真链路 + 可灰度切换",**不删 Route B**。

## 2. 关键技术发现(决定方案形态)

| 发现 | 含义 |
|---|---|
| shim 协议已定型:stdin 喂 1 个 JSON Request,stdout 吐 NDJSON 事件流(start/text/thinking/tool_use/tool_result/result/system/done/error),末行 done 带 session_id | provider 的输入/输出契约现成,只需对接 |
| `SandboxClient` 有 `exec_stream()`(逐 ExecEvent yield),但 `SandboxExecutor` Protocol 只暴露缓冲版 `exec()`(drain 到完成) | **核心落差**:缓冲 exec 会让用户等到整轮结束才见输出。必须在 executor 层加流式 exec,provider 按行解析 NDJSON |
| shim 经 `docker exec -i <ctr> /opt/cocola/shim/entrypoint.sh` 驱动,stdin = Request JSON | 经 sandbox-manager 即 `exec_stream(sandbox_id, ["/opt/cocola/shim/entrypoint.sh"], stdin=<json bytes>)` |
| `server.py` 已先 `binder.acquire()` 绑定 sandbox,把 sandbox_id 经 AgentOptions 传给 provider | session→sandbox 解析已在调用链里,provider 直接用 `options.sandbox_id` |

AgentEvent taxonomy(对齐 claude_sdk_provider,新 provider 必须一致):
text{text} / thinking{thinking} / tool_use{id,name,input} /
tool_result{tool_use_id,content,is_error} / result{...} / system{subtype,data} /
error{error} / done{}。

## 3. 改造步骤(每步独立提交 + 验证)

### Step A — executor 层补流式 exec(底座)
- `sandbox_binder.py`:
  - `SandboxExecutor` Protocol 加 `exec_stream(...) -> AsyncIterator[ExecChunk]`。
  - 新增 `ExecChunk` 轻量 dataclass(stdout 增量 / stderr 增量 / exit / error)。
  - `SandboxManagerExecutor.exec_stream`:用 anyio 把同步的
    `SandboxClient.exec_stream()` 逐 ExecEvent 桥接成 async 迭代器(不能 drain)。
  - `StaticSandboxExecutor.exec_stream`:可脚本化吐 stdout chunk + exit,供单测。
- 不动现有缓冲 `exec`(Route B 工具路径仍用)。

### Step B — 新增 InSandboxShimProvider(主体)
- 新文件 `cocola_agent_runtime/shim_provider.py`,实现 AgentProvider:
  1. 校验 `options.sandbox_id`,无沙箱不走 Route A。
  2. 组 Request JSON:{prompt, system_prompt?, max_turns, resume?};resume 来自
     session→上次 shim 返回 session_id 的内存映射(首轮无)。
  3. 调 `executor.exec_stream(sandbox_id, [SHIM_ENTRYPOINT], stdin=request_json)`。
  4. **按行缓冲 stdout 增量**(chunk 可能切断一行)→ 每整行 json.loads → 映射成
     AgentEvent(口径见上)。thinking 单独保留(shim 的 "text" → provider 的
     "thinking" key)。
  5. shim 非零退出 / error 事件 → yield 干净的 error AgentEvent。
- session_id 持久化:**本轮仅进程内内存映射**(够验证 --resume),持久化到
  ADR-0008 卷留后续。

### Step C — composition root 灰度切换
- `__main__.py::_build_provider` 选择优先级:
  1. `COCOLA_AGENT_ROUTE=A` 且 sandbox 可用 → InSandboxShimProvider(Route A)。
  2. 否则 `COCOLA_LLM_BASE_URL` 有值 → ClaudeAgentSDKProvider(Route B fallback)。
  3. 否则 → EchoProvider。
- 显式开关 `COCOLA_AGENT_ROUTE`(默认空=B):默认行为不变、可灰度、可一键回滚。
- run-stack.sh / README 加一行 Route A 开启说明。

## 4. 测试策略
- 新单测 `tests/test_shim_provider.py`(用 StaticSandboxExecutor 脚本化 NDJSON):
  ① 事件按序正确映射;② 跨 chunk 断行能正确重组;③ session_id 被捕获;
  ④ shim error / 非零退出 → 干净 error 事件。
- executor 单测:exec_stream 增量 yield + 终态 exit 覆盖。
- 端到端:`sandbox-runtime-verify.sh`(开 COCOLA_AGENT_ROUTE=A)即 Route A 真链路验收。
- 回归:现有 21 + Route B 单测必须仍全绿(证明 fallback 没坏)。

## 5. 影响文件
| 文件 | 改动 |
|---|---|
| `sandbox_binder.py` | +exec_stream(Protocol+Manager+Static)、+ExecChunk |
| `shim_provider.py` | 新增 InSandboxShimProvider |
| `__main__.py` | _build_provider 加 Route A 分支 + COCOLA_AGENT_ROUTE |
| `tests/test_shim_provider.py` | 新增 |
| `tests/test_sandbox_binding.py`(或新文件) | +exec_stream 单测 |
| `scripts/run-stack.sh` / `README.md` | +Route A 开关说明 |
| `docs/archive/feat-route-a-shim-provider.md` | 新增 changelog |

## 6. 风险与回滚
- NDJSON 跨 ExecEvent 断行:行缓冲处理 + 单测覆盖。
- 回滚:不删 Route B;取消 COCOLA_AGENT_ROUTE 即恢复。每步独立 commit。
- 跨 venv/protobuf 的 5 个预存 collection error 与本改造无关;新单测用
  StaticSandboxExecutor,不碰 protobuf 运行时。

## 7. 决策(已拍板)
1. session_id 持久化:本轮进程内内存映射。
2. 灰度:显式 COCOLA_AGENT_ROUTE=A 开关,默认走 B。
3. thinking:单独保留,对齐 claude_sdk_provider。
