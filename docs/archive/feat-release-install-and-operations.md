# feat: 收敛 v0.1 安装与运维入口

- 变更时间：2026-08-01 21:26 (+08:00)

## 变更理由

v0.1 首次公开版本需要一条足够简单的自部署路径。原 CLI 同时暴露 `up/down` 与
`start/stop`，交互安装又包含普通用户无需理解的高级配置，迫使用户在相近选项之间做选择。
开发侧的 `make dev` 与 `make web-dev` 也已具有相同行为，不应继续保留重复入口。

此外，Admin Nodes 的节点接入能力尚未完成，页面不应提前展示不可用的 Join Command；
README 的发布信息和命令示例也需要与最终 CLI 保持一致。

## 变更内容

- `apps/cli/internal/command`、`apps/cli/internal/ui`：默认交互安装使用全英文向导和 Cocola
  艺术字，聚焦管理员账号、自动生成的安全密钥与服务端口；完成页明确展示配置文件位置。
- `apps/cli/internal/command`、`apps/cli/internal/compose`：生命周期只暴露 `cocola start` 与
  `cocola stop`，移除 `up/down/restart`；`start` 负责拉取、创建、更新和恢复服务，`stop`
  Drain 运行中 Sandbox 后保留 Compose 容器、网络与数据。
- `Makefile`、`scripts/run-stack.sh`：移除与 `make dev` 重复的 `make web-dev`，开发服务统一
  通过自定义 Node server 获得热更新和 Workspace WebSocket。
- `apps/web/app/admin/sandbox-nodes/page.tsx`：Add Node 弹窗改为明确的英文 Coming Soon
  状态，不再请求、展示或复制尚不可用的节点加入命令。
- `README.md`、`docs/cli.md`、ADR、benchmark 与运维注释：统一安装、启动和停止命令，并用
  flat-square badges 展示当前发布与运行环境信息。

## 关键取舍

- `start` 每次都会检查 Docker/Compose、拉取当前版本镜像并执行幂等 `up --wait`，以一个
  命令覆盖首次部署、更新与停止后的恢复。
- `stop` 会销毁 Redis 已登记的 Sandbox 计算实例，但不会删除 Session Storage/PVC、服务
  容器或网络；恢复服务后，对话再次执行时会按需创建计算实例。
- 高级安装参数仍保留为 flags，默认向导只呈现大多数自部署用户完成安装所需的选项。
