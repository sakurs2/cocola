# ADR-0021: 独立 CLI 与 Release 引导安装器

- Status: Accepted
- Date: 2026-07-12
- Deciders: @cocola-maintainers

## Context

Cocola 的开发启动由 `make dev` 和近千行本地编排脚本承担。这些脚本包含 Native
调试、k3d、日志和历史兼容分支，适合仓库开发，但不适合作为用户部署接口。运维用户
希望不 clone 仓库即可安装，同时需要美观交互、可自动化输出和统一生命周期命令。

目标是建立一个简单可靠的正式部署入口；本轮不实现远程 SSH、Kubernetes 集群管理、
备份恢复或分布式控制面。

## Decision

1. 使用独立 Go module 构建单文件 `cocola` CLI。命令结构使用 Cobra，终端样式使用
   Lip Gloss，安装表单使用 Huh；非 TTY、`NO_COLOR` 和 JSON 输出有明确降级边界。
2. CLI 管理一份编译期内嵌的正式 Docker Compose，只引用带版本号 Release 镜像；
   不调用或包装开发脚本，不保留 Native/Container 模式分支。
3. `install` 以一个强类型配置同时驱动 flags 与交互表单，原子写入 `~/.cocola`；
   重复安装 fail-closed，不覆盖 Secret，也不启动服务。用户检查配置后通过唯一启动
   入口 `cocola up` 拉取镜像并启动。生命周期命令直接以参数数组调用 Docker，不经
   shell 拼接。
4. 公网 `install.sh` 只做最小引导：平台识别、固定 Release 下载、SHA-256 校验和
   CLI 原子安装。所有部署决策由 CLI 持有。
5. 合法且递增的 SemVer tag 一次发布 CLI 和全套同版本镜像。镜像构建前必须拒绝
   非规范、回退或已经发布的版本；CLI Release 必须等待镜像构建成功，避免出现安装器
   已经可见但对应服务镜像尚不存在的中间态。

## Alternatives Considered

- **继续扩展 shell 脚本**：无需新二进制，但交互、类型校验、跨平台测试和错误处理
  会持续散落，且会把开发态分支暴露给正式用户。
- **Python CLI**：终端库成熟，但目标机需要 Python 或打包运行时，制品更大且安装
  边界更复杂。
- **Rust CLI**：单文件和性能优秀，但 Cocola 控制面已经以 Go 为主，引入新工具链
  的维护收益不足。
- **CLI 内置 Docker SDK**：类型化程度更高，但 Compose 已经提供可靠依赖排序、
  healthcheck 和卷管理；重复实现会增加复杂度。

## Consequences

- 用户只需要 Docker 和一条 `curl | sh`，无需下载源码或安装开发工具链。
- 正式部署与源码开发职责清晰，CLI 代码可以用单元测试覆盖且输出适合人和自动化。
- 安装仍依赖 GitHub Release 与 GHCR 可达；离线包、升级迁移、备份恢复后续按独立
  能力增加，不在首版 CLI 中预留灰度分支。
