# fix: 移除不可靠的公共镜像加速选择

- Change time: 2026-08-10 23:11 (+08:00)

## Reason

中国大陆加速模式依赖公共 GHCR 与 Docker Hub 代理。真实冷启动中，代理先后对 OpenSandbox 和
MinIO 命名空间的 Manifest 请求返回 `403 Forbidden`；部分仓库可用并不能证明完整依赖集合可用，
导致用户完成一次产品级选择后仍无法稳定安装。继续逐镜像增加例外会形成难以验证和长期维护的
供应链路由表，因此移除该能力，恢复单一、明确的官方镜像来源。

## Changes

- `apps/cli/internal/config`：删除 `ImageSource` 类型与镜像代理映射，所有镜像引用固定使用 Cocola
  发布仓库或依赖项目的直接 Registry；配置 Schema 升级到 4，旧 `cn-mirror` 安装在升级准备时迁移到 GHCR/Docker Hub，
  删除 `COCOLA_IMAGE_SOURCE`，同时保留用户显式设置的 Cocola 自有镜像 Registry。
- `apps/cli/internal/command`、`apps/cli/internal/doctor`：移除安装向导、`--image-source` 参数、
  镜像源摘要、诊断项和失败切换提示；镜像缓存回退与标准故障诊断保持不变。
- `apps/cli/internal/config/*_test.go`、`apps/cli/internal/command/*_test.go`、
  `apps/cli/internal/doctor/doctor_test.go`：覆盖官方引用、新参数契约、旧配置迁移、回滚和输出语义。
- `README.md`、`docs/cli.md`：安装文档收敛为官方 Registry 与可选的 Cocola 自有镜像 Registry 覆盖。

## Tradeoffs

- 中国大陆用户需要自行提供可审计的网络出口、Docker daemon Registry 配置或预缓存镜像；Cocola
  不再内置未经完整冷启动持续验证的公共代理。
- 仅保留一个镜像解析路径，减少状态、升级分支和供应链故障面；旧加速配置通过显式 Schema 迁移
  一次性退出，不在运行时保留隐藏兼容分支。
