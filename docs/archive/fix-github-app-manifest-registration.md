# fix: Repair GitHub App Manifest registration

- 变更时间：2026-08-03 00:10 (+08:00)

## 变更理由

用户从本地 Cocola 点击 GitHub Connector 的 Register 后，GitHub 拒绝 Manifest：即使 Webhook 标记为关闭，GitHub 仍会校验其中的 localhost Hook URL；同时 Repository Variables 使用了不受支持的 `variables` 权限参数名。

## 变更内容

- `apps/gateway/internal/project/service.go`：在未消费 GitHub App Webhook 时完全省略 `hook_attributes`，并把 Variables 权限改为 GitHub 支持的 `actions_variables`。
- `apps/gateway/internal/project/service_identity_test.go`：以 localhost Origin 覆盖 Manifest 回归，验证无 Webhook 配置且权限参数有效。
