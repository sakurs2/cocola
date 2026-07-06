# fix: make dev Ctrl-C 优雅退出

- 变更时间：2026-07-06 16:47 (+08:00)

## 变更理由

`make dev` 运行中按 Ctrl-C 会触发脚本清理，但退出码仍按 SIGINT/SIGTERM 的 130/143
冒泡给 make，终端显示为命令失败。用户中断是正常停止路径，不应被展示成启动错误。

## 变更内容

- scripts/run-stack-dev.sh：新增 graceful stop 处理，收到 INT/TERM 时停止 native 子栈和
  OpenSandbox port-forward 后以 0 退出。
- scripts/run-stack-dev.sh：等待内层 `run-stack.sh` 时识别 130/143，视为用户主动停止并返回 0；
  其它非零退出码仍保留为真实失败。
- Makefile：`make dev` 对 130/143 再做一层兜底转换，避免终端显示 make error。

