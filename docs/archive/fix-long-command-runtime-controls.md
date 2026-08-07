# fix: 完善长耗时命令的进度、取消与清理链路

- 变更时间：2026-08-07 23:55 (+08:00)

## 变更理由

长耗时命令此前存在一组相互关联的体验与可靠性问题：Python 子进程的管道输出可能被缓冲，Agent Runtime 又只在 Bash 完成后返回结果，因此前端长时间没有进度；默认 600 秒的工具硬超时会终止仍在正常运行的命令；取消或超时只关闭请求流，没有精确清理 Sandbox 内的子进程树；对话框运行态没有可用的停止入口；终态写入还可能用流内的零值覆盖已经统计到的 LLM 调用数。Sandbox 镜像同时缺少常用的 `pip` / `pip3` 命令。

另外，本地重复启动 `make dev` 时，旧 supervisor 的退出清理可能读取到新实例写入的共享 PID 文件，从而误杀新的 OpenSandbox 端口转发。

## 变更内容

- `deploy/sandbox-runtime/`、`apps/agent-runtime/`：安装并验证 `python -m pip`、`pip`、`pip3`，默认启用 Python 无缓冲输出；为 Claude Bash 和 Codex command execution 统一生成增量 `tool_output` 事件，保持原 stdout/stderr 与退出码不变。
- `apps/gateway/internal/convo/`、`apps/web/`：持久化并实时归并最多 64 KiB 的工具输出；使用 HeroUI Card、Button、Tooltip 和 ScrollShadow 展示紧凑的长命令活动卡片，支持运行状态、耗时、最新输出与展开详情。
- `apps/web/app/runtime-provider.tsx`、`apps/web/components/assistant-ui/thread.tsx`：增加运行态停止按钮，通过 Run DELETE 接口取消，并继续跟随服务端流直到权威终态，随后用持久化消息校准统计和局部状态。
- `apps/sandbox-manager/internal/provider/opensandbox/`：为每次执行创建独立进程组和不可混淆的 PGID 标记；取消或超时后先 TERM、宽限等待，再 KILL 该进程组，不影响 code-server 等无关 Workspace 进程。
- `apps/gateway/internal/chatrun/`：Finalize 采用单调递增的 LLM 调用数，避免超时或取消路径用零值覆盖权威统计。
- `apps/admin-api/`、`apps/gateway/`、`.env.example`、`docs/configuration.md`：将单工具默认硬上限调整为 3600 秒（仍可在 Admin Settings 配置，最大 86400 秒），明确静默不等于超时。
- `scripts/run-stack-dev.sh`、`scripts/run-stack-dev-test.sh`：端口转发清理绑定到当前 supervisor 持有的 PID，防止旧实例误杀新实例。
- 对 Go、Python、Node、Shell 和 Web 类型/生产构建补充或更新测试，覆盖增量输出、进程组清理、取消行为、统计保护和 PID 所有权。

## 关键取舍

- 增量输出协议是通用 `tool_output`，不绑定训练场景；Python 无缓冲只是兼容增强，其他语言和命令同样走统一事件与卡片链路。
- 超时仍保留为安全硬上限，长命令不会因为一段时间没有输出而被判定失败；用户可随时主动停止。
- 进程清理只针对本次执行记录的进程组，避免按名称或全局扫描误杀用户服务。
