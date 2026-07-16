# fix: Storage Probe 无法测量私有 Runtime 目录

- 变更时间：2026-07-16 20:58 (+08:00)

## 变更理由

Admin 点击 Session Storage 的 Measure 后返回 `storage measurement failed`。节点存储
根目录可能是 `root:root 0700`，而 Claude/Codex Runtime 会创建归 `10001:10001`
所有的 `0700/0600` 状态。探针以 root 运行但删除了全部 capabilities，因此只能进入
第一层，遍历用户私有目录时会收到 `permission denied`。

## 变更内容

- `deploy/opensandbox-k8s/cocola-storage-probe.yaml`：在保留只读 hostPath、只读 rootfs
  和 `drop: ALL` 的基础上，仅补回只读目录遍历所需的 `DAC_READ_SEARCH`。
- `apps/admin-api/cmd/storage-probe/main.go`：权限错误返回明确的 403，并记录具体的服务端
  错误，避免所有遍历失败都退化为无信息量的 500。
- `apps/admin-api/cmd/storage-probe/main_test.go`：覆盖 permission、missing、timeout 和
  fallback 错误映射。

关键取舍：不使用更宽泛的 `DAC_OVERRIDE`，不开放写挂载，不增加后台扫描，也不改变
Session 目录本身的私有权限。
