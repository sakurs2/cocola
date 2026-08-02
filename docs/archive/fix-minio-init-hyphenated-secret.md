# fix: 修复连字符开头的 MinIO 密钥导致初始化失败

- 变更时间：2026-08-02 13:51 (+08:00)

## 变更理由

CLI 使用 URL-safe Base64 生成 MinIO 根密钥，该字符集允许密钥以 `-` 开头。生产 Compose 将密钥作为 `mc alias set` 的位置参数传入，但没有显式终止命令行 flag 解析；当密钥以 `-` 开头时，`mc` 会把它误认为未知 flag，导致 `minio-init` 退出 1，后续 Admin、Gateway 和 Web 服务无法启动。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`：在 `mc alias set` 的位置参数前加入 `--`，使任意合法生成密钥都按凭据处理。
- `apps/cli/internal/assets/assets_test.go`：增加回归约束，确保生产 Compose 始终保留该参数终止符。

不改变密钥格式、用户配置或 MinIO 数据卷；已有失败安装在升级 CLI 后重新执行 `cocola install` 与 `cocola start` 即可应用修复。
