# feat(agent-runtime): Route A —— InSandboxShimProvider(灰度,Route B 保留)

落地 ADR-0009 的 Route A 切换前两步:把整个 Claude Code brain 放进用户沙箱,
agent-runtime 退化为控制面路由。Route B(ClaudeAgentSDKProvider)保留为
fallback,通过显式开关 `COCOLA_AGENT_ROUTE=A` 灰度启用,默认行为不变。

方案详见 `docs/plan/route-a-shim-provider.md`。

## 改动

- `sandbox_binder.py`:
  - 新增 `ExecChunk`(流式 exec 的增量帧:stdout/stderr/exit/error)+
    `_exec_event_to_chunk`(proto ExecEvent→ExecChunk,在此解码 bytes)。
  - `SandboxExecutor` Protocol 新增 `exec_stream(...)`,流式版的 exec。
  - `SandboxManagerExecutor.exec_stream`:用 anyio memory channel(bounded 0,
    带背压)把阻塞的 `SandboxClient.exec_stream()` 逐 ExecEvent 桥接成 async
    迭代器,不缓冲整轮。
  - `StaticSandboxExecutor`:新增 `stream_handler` 钩子 + `exec_stream` 实现,
    可脚本化吐 NDJSON chunk,供单测;默认回显命令。
  - 缓冲版 `exec` 不变(Route B 工具路径继续用)。

- `shim_provider.py`(新增)`InSandboxShimProvider`:
  - 组 Request JSON {prompt, system_prompt?, max_turns, resume?},经
    `exec_stream` 把 prompt 喂给沙箱内 shim(`/opt/cocola/shim/entrypoint.sh`)。
  - **按行缓冲**重组 NDJSON(byte chunk 可能切断一行),逐行映射成 AgentEvent,
    taxonomy 与 ClaudeAgentSDKProvider 完全一致(text/thinking/tool_use/
    tool_result/result/system/error/done)。
  - 捕获 `done` 的 session_id 存进**进程内** session→session_id 映射,下轮作
    `--resume`(持久化到 ADR-0008 卷留后续)。
  - shim error 事件 / 非零退出 / 传输异常 → 干净的 error AgentEvent + 终态 done。
  - 无 `sandbox_id` 时拒绝运行(Route A 必须有沙箱),不静默降级。

- `__main__.py`:`_build_provider` 新增 Route A 分支——`COCOLA_AGENT_ROUTE=A`
  且 executor 可用 → InSandboxShimProvider;否则回落 Route B / Echo。env 列表
  补充开关说明。

- `scripts/run-stack.sh`:透传 `COCOLA_AGENT_ROUTE`。

## 测试

- `tests/test_shim_provider.py`(新增 6 项):事件按序映射 + 跨 chunk 断行重组、
  session_id 复用为下轮 resume、非零退出→终态 error、shim error 事件透传、
  无沙箱拒绝运行、Static exec_stream 默认回显。
- agent-runtime 全量 **46 passed**;ruff check / format 全绿。
- 端到端:`sandbox-runtime-verify.sh`(开 COCOLA_AGENT_ROUTE=A)即 Route A 真链路
  验收(需 Docker + gateway 环境,按需运行)。

## 范围与回滚

- 本次不删 Route B、不做 egress allowlist、不改 ADR-0009 状态(步骤 3–5,下轮)。
- 回滚:取消 `COCOLA_AGENT_ROUTE` 即恢复 Route B;每步独立、可 revert。

## 如何启用(灰度)

```bash
# 需要 sandbox-manager(提供 COCOLA_SANDBOX_ADDR)+ Route A 沙箱镜像
COCOLA_AGENT_ROUTE=A COCOLA_SANDBOX_ADDR=<addr> make up-all
```
