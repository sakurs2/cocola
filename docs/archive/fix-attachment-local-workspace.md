# fix: 附件上传本地联调找不到文件(无沙箱时落本地 workspace + 透传 SDK cwd)

日期：2026-07-01 · 关联 ADR：docs/adr/0017-attachment-storage-and-sandbox-delivery.md

## 背景
本地联调(`make up-all` → `scripts/run-stack.sh --all`)不起 sandbox-manager:
`COCOLA_SANDBOX_ADDR` / `COCOLA_AGENT_ROUTE` 均为空。由此:

1. `_build_executor()` 返回 None → `_provision_attachments` 命中 `executor is None`
   分支,**静默丢弃**上传文件(仅告警,不落盘、不加前言);
2. provider 落到进程内 `ClaudeAgentSDKProvider`(大脑跑在 agent-runtime 进程,
   而非沙箱),即便文件写进了某个沙箱,这个大脑也读不到。

两者叠加 → 用户「附带文件后提问,模型找不到文件」。

## 改动
预置目标改为「跟随大脑运行的位置」(ADR-0017):

### agent-runtime(apps/agent-runtime)
- `agent_provider.py`:`AgentOptions` 新增 `workspace: str | None`。当大脑跑在
  本进程时,provider 用它作为 SDK 的 cwd,使原生 Read/Bash 能解析 `./uploads/`。
- `server.py`:`_provision_attachments` 改返回 `(preamble, workspace)`,按分支:
  - **Route A(executor + 已绑沙箱)**:`_provision_into_sandbox`,行为不变 —
    经 `pwd` 解析沙箱 cwd,`mkdir -p uploads` 后 `write_bytes` 落
    `/workspace/<session_id>/uploads/`;`workspace` 返回 None(大脑自带该 cwd)。
  - **本地(无 executor/沙箱)**:`_provision_onto_host`,把附件写进按会话隔离的
    **宿主机目录**(`COCOLA_LOCAL_WORKSPACE_ROOT` 或系统临时目录下的
    `cocola-workspaces/<session_id>/uploads/`),并把该目录作为 `workspace` 回传。
  - 抽出 `_uploads_preamble`,两条路径共用同一段前言(相对路径一致)。
  - Query 把 `workspace` 透传进 `AgentOptions`。
- `claude_sdk_provider.py`:`_build_options` 在 `options.workspace` 存在时
  设 `ClaudeAgentOptions(cwd=...)`(该参数经 inspect 确认存在)。
- `tests/test_attachment_provisioning.py`:原 `test_no_executor_drops_attachments_but_runs`
  改写为 `test_no_executor_lands_on_host_and_sets_cwd`(断言落宿主机 + cwd 透传 +
  前言 + 二进制安全)并新增 `test_no_executor_defaults_workspace_root`(默认根回退)。

## 校验
- `pytest -q` → 56 passed, 2 skipped(较前 +1)。
- `ruff check` → All checks passed。

## 非目标
- Route A / 生产路径行为不变;OSS 持久化、pull(B)仍留 TODO(见 ADR-0017)。
