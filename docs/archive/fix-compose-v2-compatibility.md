# fix: 兼容更早的 Docker Compose v2

- 变更时间：2026-08-01 22:40 (+08:00)

## 变更理由

Cocola 的正式 Compose 使用 `configs.content` 内联 OpenSandbox 配置，该字段直到 Docker
Compose 2.23.1 才可用。它把 CLI 的最低版本门槛抬得过高，使部分已经安装 Compose v2 的
服务器仍需要额外升级环境，偏离开箱即用的安装目标。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`：移除较新的 `configs.content` 和冗余顶层 `name`，
  改为挂载 CLI 生成的 `opensandbox.toml`。
- `apps/cli/internal/config`：安装时原子生成 OpenSandbox 配置文件，并安全写入当前 Session
  Storage 宿主路径。
- `apps/cli/internal/compose`：最低 Compose 版本下调至 2.1.1，即当前实际使用的
  `docker compose up --wait` 所需版本。
- `README.md`、`docs/cli.md`：同步最低版本和安装目录结构。

## 关键取舍

- 保留 `up --wait` 的健康检查语义，不通过取消启动校验来虚假兼容更老版本。
- 新增的配置文件不包含 Secret，仍与 `compose.yaml` 一同由 CLI 管理，用户无需手工创建或
  修改。
