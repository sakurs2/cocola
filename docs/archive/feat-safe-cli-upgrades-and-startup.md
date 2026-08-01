# feat: 补齐 CLI 安全升级与生产启动闭环

- 变更时间：2026-08-01 23:27 (+08:00)

## 变更理由

重复执行在线安装脚本时，旧实现会先替换 CLI，再因已有 `config.env` 而中止，导致新版 CLI
继续管理旧版 Compose、OpenSandbox 配置和镜像版本。配置没有独立 Schema、迁移备份和
启动成功提交点，新版本增加配置项时只能要求用户手工编辑，升级失败也没有可靠恢复路径。

此外，`cocola start` 每次都强制拉取镜像，使已停止的本地部署在 Registry 暂时不可用时也
无法恢复；多个基础服务使用浮动 tag，一次普通重启可能隐式升级依赖。端口、磁盘和 Compose
配置检查发生得过晚，成功输出也没有明确的访问地址和首次模型配置入口。

## 变更内容

- `apps/cli/internal/config`：为 `state.json` 增加独立配置 Schema、部署资源指纹、最后成功
  版本和 pending upgrade 状态；新增保留 Secret、自定义环境变量、引号与注释的原子迁移，
  并在写入失败时恢复原文件。
- `apps/cli/internal/config`、`apps/cli/internal/command/install.go`：重复 `install` 自动跳过
  首次向导，备份 `config.env`、Compose、OpenSandbox 配置与 state，更新 CLI 管理文件并提示
  用户运行原有的 `cocola start`，不新增日常升级命令。
- `apps/cli/internal/command/lifecycle.go`、`apps/cli/internal/compose`：首次启动或待升级时拉取
  目标镜像，Registry 失败但目标镜像完整缓存时继续；普通停止后恢复直接使用本地镜像。
- `apps/cli/internal/command/lifecycle.go`、`apps/cli/internal/config`：升级前若已有 PostgreSQL
  数据卷则生成 owner-only 的压缩 dump；目标版本启动失败时恢复旧部署文件但不继续编排容器，
  提示用户检查错误后显式执行 `cocola start` 恢复上一版本，同时永久保留部署与数据库备份。
- `apps/cli/internal/command/preflight.go`、`apps/cli/internal/compose/runner.go`、
  `apps/cli/internal/doctor`：镜像操作前检查 Docker/Compose、配置 Schema、Compose 配置、
  首次启动端口和磁盘空间，并保留 Docker daemon 的具体错误。
- `apps/cli/internal/assets/compose.yaml`：固定 Redis、PostgreSQL、MinIO、MinIO Client 和
  OpenSandbox 默认版本；为 Web 增加真实 HTTP 健康检查。
- `apps/cli/internal/*_test.go`：使用假 Docker 覆盖配置迁移、Secret 保留、精确回滚、缓存镜像
  降级、数据库备份、端口冲突和升级启动失败恢复，不运行真实部署。
- `README.md`、`docs/cli.md`：记录无新增命令的升级流程、备份目录、镜像策略、前置检查和
  启动结果。

## 关键取舍

- 配置迁移和容器切换分成两阶段：`install` 只准备升级，`start` 在健康检查全部通过后才提交
  新版本状态，避免“配置已升级但服务未成功”的假完成。
- 自动回滚只恢复 CLI 管理的部署文件，不在失败分支中再次启动服务，也不自动覆盖持久数据；
  数据库 dump 保留在升级备份目录，避免失败处理本身造成不可逆数据损坏。
- 普通恢复不主动检查 Registry；目标版本首次需要下载时仍会拉取，只有已确认所有目标镜像
  均存在本地时才允许在拉取失败后继续。
