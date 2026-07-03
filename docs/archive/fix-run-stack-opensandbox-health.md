# fix: make up 提前校验 OpenSandbox server 健康状态

- 变更时间：2026-07-03 21:02 (+08:00)

## 变更理由

执行 `make up` 后，前端对话报错 `dial tcp 127.0.0.1:8090: connect: connection refused`。
触发条件是 OpenSandbox server 没有真正监听宿主 8090，但 `scripts/run-stack.sh`
启动 Docker-runtime OpenSandbox 时吞掉了 compose 启动失败，并且没有在健康检查超时后中断，
导致 sandbox-manager 继续启动，直到用户发起对话才暴露错误。

## 变更内容

- scripts/run-stack.sh：补充 OrbStack/Homebrew 常见 PATH；OpenSandbox compose 启动失败时立即报错；
  健康检查超时后打印 OpenSandbox 日志并退出；外部 OpenSandbox 模式也会预先检查 `/health`。
- scripts/docker-compose.sh：补充 OrbStack/Homebrew 常见 PATH，避免不同终端环境找不到 Docker CLI。
