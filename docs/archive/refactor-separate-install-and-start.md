# refactor: 分离生产配置安装与服务启动

- 变更时间：2026-07-13 00:06 (+08:00)

## 变更理由

`cocola install` 原先默认在生成配置后立即拉取镜像并启动服务。用户无法在首次启动前
完整检查或调整生成的 `config.env`，同时 `install` 的“仅配置”和“配置后启动”两套
行为增加了命令语义和失败中间态。

## 变更内容

- `apps/cli/internal/command/install.go`：删除安装阶段的 Docker 检查、镜像拉取和服务
  启动，移除 `--start` 选项及交互式启动询问；安装完成后提示用户检查配置并执行
  `cocola up`。
- `apps/cli/internal/config/config.go`：删除只服务于安装自动启动的 `Start` 配置字段。
- `apps/cli/internal/command/root.go`、`root_test.go`：明确 `install` 与 `up` 的职责并覆盖
  下一步提示。
- `README.md`、`docs/cli.md`、`docs/adr/0021-unified-cli-and-bootstrap-installer.md`：同步
  两阶段生产部署流程。

## 关键取舍

不保留 `--start` 或 `--no-start` 两套路径。`install` 是唯一配置初始化入口，`up` 是
唯一生产启动入口，使失败边界和用户操作都保持简单、可预测。
