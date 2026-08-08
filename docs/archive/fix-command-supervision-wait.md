# fix: 修复命令监督器提前完成与空回复

- 变更时间：2026-08-08 10:17 (+08:00)

## 变更理由

长耗时命令进程树清理使用 `setsid sh -c` 为每次 Sandbox 执行创建独立进程组。Linux `setsid` 在调用者已经是进程组长时会先 fork；未指定 `--wait` 时，父进程立即返回，OpenSandbox 因而在约 2ms 内把 Agent shim 判定为成功完成，真实模型进程却继续在后台运行。Gateway 最终写入了 `status=success`、`tool_call_count=0`、`parts=null` 的空 assistant 消息，用户看到模型没有回答。

本地开发脚本也使用相同的 `setsid` 前缀启动服务和端口转发，在提供 `setsid` 的 Linux 环境存在同类 supervisor 生命周期脱离风险。既有测试只检查生成的命令字符串，没有覆盖 `setsid` 作为进程组长时的真实 fork 行为。

## 变更内容

- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`：执行包装改为 `setsid --wait sh -c`，等待真实命令结束并透传退出状态，同时保留唯一 PGID marker、TERM 宽限和 KILL 兜底。
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox_linux_test.go`：在 Linux 上强制让 `setsid` 成为进程组长，验证 fork 分支仍等待子进程并透传非零退出码；既有协议测试同步锁定 `--wait`。
- `scripts/run-stack.sh`、`scripts/run-stack-dev.sh`：仅在本机 `setsid --wait` 可用时通过 `exec_in_session` 接管进程，否则回退到 Bash job control；既让 `$!` 始终对应真实进程，又兼容 macOS Bash 3.2，避免服务与 OpenSandbox 端口转发脱离 supervisor。
- `apps/gateway/internal/httpapi/simple_chat.go`：成功 run 必须包含用户可见的文本、工具调用、Artifact、Plan、问题、审批或结构化结果；仅环境/状态/记忆/进度/推理或完全无内容时，落库为 `EMPTY_AGENT_RESPONSE` 并展示明确错误，不再生成空成功消息。
- `apps/gateway/internal/httpapi/simple_chat_test.go`：覆盖运行时脚手架与真实回答的判定边界、空 Agent 响应的 SSE 和持久化终态，并保持最终写入恢复测试的原始成功语义。
- `docs/adr/0027-sandbox-command-supervision.md`：提出长期专用 `cocola-exec` 监督器方案，将结构化启动、进程身份、输出、等待、超时与取消收敛为版本化 Sandbox Runtime 合约；进程组为可移植基线，cgroup v2 为可用时的强化层。

## 验证

- sandbox-manager 全量 Go 测试通过。
- Gateway 全量 Go 测试通过。
- `run-stack-dev` Shell 语法与行为测试通过。
- `make dev` 完整栈重启成功，Web `:3000`、OpenSandbox `:8090` 与端口转发均在线。
- 通过 sandbox-manager `Acquire -> Exec -> Release` 执行真实 Linux Sandbox 命令 `sleep 1; ...; exit 23`：调用等待 2.09 秒，收到完整 stdout 与退出码 23，并在 `finally` 中释放测试 Sandbox，证明不再提前伪成功。
