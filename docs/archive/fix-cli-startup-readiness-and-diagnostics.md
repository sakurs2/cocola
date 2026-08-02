# fix: 收紧 CLI 启动就绪条件并补齐诊断闭环

- 变更时间：2026-08-02 17:34 (+08:00)

## 变更理由

生产 Compose 原先只等待 Sandbox Manager 容器进入 started，未确认其完成 Redis、Provider、
存储和 gRPC 初始化；CLI 拉取的也只有 Compose 服务镜像，Sandbox Runtime、OpenSandbox
execd 和 egress 要到第一次创建沙箱时才下载。因此 `cocola start` 可能已经显示 ready，但实际
Agent 工作仍会因服务未就绪、镜像网络或磁盘问题失败。

删除配置后重新安装还可能继续挂载旧的 PostgreSQL 数据卷。原健康检查不验证密码，导致数据库
被标记 healthy，后续服务才因新配置 Secret 与旧卷凭据不一致而失败。升级失败提示和现有
`doctor` 也不足以区分恢复旧版、重试升级、容器健康、遗留数据卷及缺失镜像。同时多个 CLI
变更命令可以并发修改同一安装目录，存在配置状态与 Compose 操作竞争。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`：为 Sandbox Manager 增加现有 `/healthz` 探针并让
  Agent Runtime 等待 `service_healthy`；PostgreSQL 改为带当前配置密码的真实 SQL 健康检查。
- `apps/cli/internal/compose/runner.go`：首次或升级拉取时预取 Sandbox Runtime、execd 和
  egress，将它们纳入离线缓存完整性检查；增加服务状态、缺失镜像、Docker Root Dir 和
  PostgreSQL 凭据诊断能力。
- `apps/cli/internal/command/lifecycle.go`：首次启动发现已有 PostgreSQL 卷时先验证当前凭据，
  兼容的中途安装继续启动，不兼容的遗留卷停止并提供保留或显式清理数据的路径；升级失败同时
  显示恢复旧版和重新准备目标升级的命令。
- `apps/cli/internal/operationlock`、`apps/cli/internal/command`：使用安装目录级 advisory file
  lock 串行化 `install`、`start` 和 `stop`，锁竞争时显示正在运行的命令、PID 与开始时间；
  `status`、`logs` 和 `doctor` 保持只读可用。
- `apps/cli/internal/doctor`、`apps/cli/internal/host`：doctor 增加 pass/warning/fail 状态，检查
  Compose 容器健康、一次性初始化服务、安装与 Docker 存储磁盘、具名数据卷、PostgreSQL
  凭据和全部必需镜像；诊断过程不启动容器、不拉取镜像、不删除资源。
- `apps/cli/internal/*_test.go`：扩充 fake-Docker 回归测试，覆盖依赖健康、运行时镜像预拉取、
  遗留数据库拒绝、升级重试提示、服务诊断和并发锁。
- `README.md`、`docs/cli.md`：同步新的启动就绪条件、升级恢复路径、操作锁和 doctor 能力。

## 关键取舍

- 不自动删除或改写已有 PostgreSQL 数据卷；只有用户明确选择放弃数据时才按提示手工删除。
- 外部 OpenSandbox 的运行时镜像由远端环境管理，CLI 只在默认 Managed OpenSandbox 模式下
  预拉取本地主机所需镜像。
- 操作锁按 `--home` 隔离并由操作系统在进程退出时释放，不增加新命令、配置项或清理步骤。
