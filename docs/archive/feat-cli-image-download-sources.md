# feat: 统一 Cocola 容器镜像下载源

- Change time: 2026-08-10 15:28 (+08:00)

## Reason

中国大陆网络环境直接访问 GHCR、Docker Hub 和 Codeberg 时可能速度很慢或无法完成冷启动。
原有部署又混用了短镜像名、固定第三方地址和代码内 Runtime sidecar 常量，用户无法用一次选择
切换完整供应链，离线缓存检查也无法稳定描述实际需要的全部镜像。升级准备页同时显示
`Current version` 与 `Target version`，容易让用户误以为尚未完成或升级结果不明确。

## Changes

- `apps/cli/internal/config/`：增加 `cn-mirror` 与 `direct` 两种类型化镜像源；新安装默认中国
  加速，历史 Schema 迁移为直连；保存镜像源及 Sandbox、execd、egress 的有效引用，并支持
  同版本切源的两阶段提交和精确回滚。
- `apps/cli/internal/assets/compose.yaml`：Redis、PostgreSQL、Forgejo、MinIO、OpenViking 和
  OpenSandbox 全部改用 CLI 生成的显式完整镜像引用；OpenViking 与 Forgejo 同时固定版本和 digest。
- `apps/cli/internal/command/`、`apps/cli/internal/doctor/`：首次向导和 `--image-source` 只暴露
  一次产品级选择；启动与诊断显示当前镜像源；代理失败不自动回退，缓存完整时仍允许离线启动。
- `.github/workflows/release.yml`、`THIRD_PARTY_NOTICES.md`：使用固定 regctl 版本原样同步 Forgejo
  多架构镜像，拒绝覆盖不同 digest，验证匿名读取，并将对应 commit 的完整源码和 GPL 许可证
  随 Cocola Release 一起发布。
- `apps/cli/internal/command/`：升级准备阶段使用 `Before version / New version`，健康检查成功后
  使用 `Before version / Current version`；同版本切源只显示镜像源变化。
- `docs/cli.md`、`README.md`：记录镜像下载源、显式切换、失败语义和 Release 合规门槛。

## Tradeoffs

- 不修改 Docker daemon，也不做地域探测或静默回退，避免隐藏供应链变化；代理不可用且缓存
  不完整时要求用户显式切换到 `direct`。
- `COCOLA_IMAGE_REGISTRY` 只覆盖 Cocola 自有镜像，第三方镜像继续由统一下载源确定，既保留
  高级部署能力，也避免重新引入逐镜像配置路径。
- Forgejo 镜像保持上游 Manifest 字节不变，因此许可证和未修改声明通过仓库、工作流摘要及
  Release 源码资产提供，不通过重建镜像修改 OCI 配置。
