# fix: 优雅退出时清理 Warm Pool

- 变更时间：2026-07-12 12:45 (+08:00)

## 变更理由

`make dev` 退出时会等待 Sandbox Manager 把 active session checkpoint 上传到 MinIO，但
未领取的 Warm Pool sandbox 会继续留在 k3d，占用本地资源，并可能在下次启动时携带旧镜像或
旧创建期配置。直接执行 `kubectl delete batchsandbox --all` 会同时删除已绑定的 active
session sandbox；按 Kubernetes 创建期 warm 标签删除也不安全，因为 sandbox 被 claim 后标签
不会自动更新。

## 变更内容

- `apps/sandbox-manager/internal/orchestrator/warmpool.go`：新增 Warm Pool drain，依据 Redis 中
  权威 inventory 只销毁仍未被领取的 warm sandbox；provider 删除失败时保留 inventory key，
  便于后续重试。
- `apps/sandbox-manager/cmd/sandbox-manager/main.go`：优雅退出先停止接单并完成 active session
  checkpoint，然后停止并等待 warm refill loop，最后在 5 秒预算内 drain Warm Pool。
- `apps/sandbox-manager/internal/orchestrator/binder_test.go`：验证 drain 只删除未领取库存，不会删除
  已经 claim 并绑定会话的 sandbox。

关键取舍：不使用 `kubectl --all`、Kubernetes label selector 或新增开关；由 Warm Pool 的拥有者
Sandbox Manager 清理自身状态，同时保持 checkpoint 优先、Warm Pool 删除最后执行。
