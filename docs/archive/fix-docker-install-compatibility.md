# fix: 明确 Docker 兼容边界并修复首次部署等待失败

- Change time: 2026-08-20 17:11 (+08:00)

## Reason

一台远端服务器需要多次重试才能完成 Cocola 安装。排查确认故障按顺序暴露了多个独立层次：

1. 已有安装使用旧配置 Schema，必须先重新执行 `cocola install` 完成迁移；仅重试
   `cocola start` 无法修复配置。
2. 宿主机原 Docker Engine 为 20.10.24，最初被怀疑过旧，但后续隔离测试证明该版本可以完成
   Cocola managed 全栈健康启动和动态 Sandbox 创建、执行、销毁；Engine 升级不是安装成功的
   必要条件，原有判断缺少实测支持。
3. Engine 升级后，系统已安装 Compose 5.5.0，但
   `~/.docker/cli-plugins/docker-compose` 中的 Compose 2.40.2 具有更高查找优先级，实际命令仍
   使用旧用户插件。只查看系统包版本会误判生效版本。
4. 正式 Compose 对尚未配置模型的 OpenViking 使用 `healthcheck: disable: true`。Compose
   2.40.2 配合 `docker compose up --wait` 会以“没有配置 healthcheck”退出，而同一配置在
   Compose 5.5.0 可以成功。因此安装依赖不仅是版本字符串，也包含 Compose 的实际运行语义。

每次重试只解除了一层阻塞，下一层问题才会显现；Engine 升级也不会自动移除优先级更高的用户级
Compose 插件，这就是升级后仍然失败、最终需要继续排查插件路径的原因。

## Changes

- `apps/cli/internal/compose/runner.go`：在启动前要求 Docker Engine 20.10.0 或更高版本，并保留
  Compose 2.23.1 最低版本检查；提供实际生效 Compose 插件路径探测。
- `apps/cli/internal/doctor/doctor.go`：显示 Engine 版本、Compose 版本和插件路径，并把
  OpenViking 纳入必需服务检查。
- `apps/cli/internal/command`：旧配置 Schema 阻止启动时显示检测版本、要求版本、实际安装目录，
  并给出可直接执行的 `cocola install --home ...` 和后续 `cocola start --home ...`；JSON 模式
  同步返回稳定错误码、版本字段与下一步命令。
- `apps/cli/internal/assets/compose.yaml`：用与模型配置无关的 OpenViking 进程存活检查替代禁用
  healthcheck，使首次部署可以在管理员尚未配置模型时通过 `compose up --wait`。
- `apps/cli/internal/compose/testdata/compatibility.yaml`、
  `scripts/verify-docker-compatibility.sh`、`.github/workflows/ci.yml`：新增最低组合
  Engine 20.10.0 / Compose 2.23.1 与当前组合 Engine 29.7.2 / Compose 5.5.0 的真实 Compose
  探针，覆盖内联配置、host-gateway、健康检查和 `up --wait`。
- `apps/web/messages/{en,zh-CN}/chat.json`：未配置模型时统一引导用户前往管理员页面配置模型。
- `README.md`、`docs/cli.md`：记录最低版本、支持策略、CI 矩阵和用户级 Compose 插件覆盖风险。
- 相关 Go 与 Web 测试：覆盖最低版本拒绝、插件路径、OpenViking 存活检查和中英文提示。

## Tradeoffs

- 用户主机只检查最低 Engine 与 Compose 版本，不要求匹配 CI 的精确版本，避免不必要地限制较新
  稳定版本。
- Engine 19.03.15 的真实容器探针会因不支持 `host-gateway` 失败；Engine 20.10.0 与 Compose
  2.23.1 的兼容探针通过。Engine 20.10.24 与 Compose 2.23.1 另行完成了生产 Compose、全部
  服务健康检查和动态 Sandbox 生命周期验证，因此不把 Engine 25 作为安装硬门槛。
- CI 不穷举所有版本笛卡尔积，只验证最低边界与当前维护基准；这能控制运行成本，同时对最低
  版本承诺和新版本回归提供直接证据。
- OpenViking 健康检查表示部署进程存活，不表示模型相关服务已经就绪。模型可用性继续由应用层
  配置与请求诊断负责，避免首次安装被尚未完成的管理员配置阻塞。
