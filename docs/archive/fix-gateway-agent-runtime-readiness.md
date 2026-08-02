# fix: Gateway 等待 Agent Runtime 就绪后启动

- 变更时间：2026-08-02 16:47 (+08:00)

## 变更理由

生产部署同时创建 Agent Runtime 和 Gateway 时，Compose 的 `service_started` 只保证 Runtime 容器进程已经启动，不保证其 gRPC 端口已经监听。Gateway 可能在这段窗口内连接失败并退出；即使 restart policy 随后使 Gateway 恢复健康，`docker compose up --wait` 也已经返回依赖启动失败，Web 因而停留在 `Created`。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`：为 Agent Runtime 增加本地 gRPC 端口 TCP 健康检查，并将 Gateway 依赖条件改为 `service_healthy`。
- `apps/cli/internal/assets/assets_test.go`：增加回归约束，确保生产 Compose 始终探测 Runtime 就绪状态并让 Gateway 等待健康结果。

健康检查使用 Agent Runtime 镜像内已有的 Python 标准库，不增加镜像依赖或用户配置项。Gateway 的运行逻辑和 restart policy 保持不变。
