# fix: Restore terminal routing in Compose deployments

- 变更时间：2026-08-03 00:10 (+08:00)

## 变更理由

生产 Compose 部署中的 Gateway 没有配置 Sandbox Manager 地址，因此对话侧边栏 Shell 无法解析沙箱终端，页面显示 terminal is not configured。开发环境已有对应链路，但发布 Compose 缺少这项依赖和环境变量。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`：Gateway 等待 Sandbox Manager 健康，并通过 `COCOLA_SANDBOX_ADDR` 连接其 gRPC 服务。
- `apps/cli/internal/assets/assets_test.go`：增加生产 Compose 终端解析配置的回归测试。
