# refactor: 恢复现代 Docker Compose 部署配置

- 变更时间：2026-08-02 00:23 (+08:00)

## 变更理由

此前为了兼容较早的 Compose v2，CLI 将 OpenSandbox 配置拆成了额外生成的
`opensandbox.toml`。实际部署故障来自服务器没有安装 Compose，而不是 Compose 版本过低；
继续维护独立配置文件会增加安装、升级、备份和回滚链路的复杂度。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`：恢复 `configs.content`，将 OpenSandbox 配置内联到
  CLI 生成的 Compose 文件。
- `apps/cli/internal/config`：停止生成和追踪独立的 `opensandbox.toml`，部署修订只由
  `compose.yaml` 决定。
- `apps/cli/internal/compose`：最低版本恢复为 Docker Compose 2.23.1；未安装或版本过旧时
  保留底层诊断并提示安装或升级后重试。
- `README.md`、`docs/cli.md`：同步最低版本和安装目录说明。
- 旧安装遗留的 `opensandbox.toml` 不会被新 Compose 引用，也不会在升级时主动删除，避免
  对现有部署文件做不必要的破坏性操作。
