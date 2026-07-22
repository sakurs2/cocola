# fix: 初始化 Sandbox 用户工具目录

- 变更时间：2026-07-23 01:40 (+08:00)

## 变更理由

Sandbox 将 `/home/cocola/.local` 映射到 Session Volume 的 `/session/home/local`，但启动时只创建了 prefix 根目录。`npx` 在执行 `create-next-app` 前检查全局 prefix 的 `lib` 目录，因此因 `/session/home/local/lib` 不存在而以 `ENOENT` 退出。默认的 `go install` 目标也未落入持久化且已加入 PATH 的目录。

## 变更内容

- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`：显式创建并授权 `.local` 下的 `bin`、npm `lib/node_modules`、pnpm 和 man 目录及其父目录。
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`：验证 Session entrypoint 同时创建并授权完整的用户工具目录契约。
- `deploy/sandbox-runtime/Dockerfile`：为直接 Docker 运行准备相同目录，并将 `GOBIN` 指向持久化的 `/home/cocola/.local/bin`。
- `scripts/sandbox-runtime-verify.sh`：离线检查包管理器目录以 `cocola` 用户可写，并验证 `GOBIN` 配置。
- 不预创建 Python 版本相关目录或工具内部状态目录，继续由相应工具按需管理。
