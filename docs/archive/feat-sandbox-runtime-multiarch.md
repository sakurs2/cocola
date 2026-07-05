# feat: sandbox runtime multiarch image

- 变更时间：2026-07-05 23:20 (+08:00)
- 关联提交：待提交

## 变更理由

GHCR 上的 `cocola-sandbox-runtime:latest` 之前只包含 `linux/amd64` manifest，
Apple Silicon Mac 默认 `docker pull` 会寻找 `linux/arm64` 版本并失败。为了让本地
开发机和 amd64 生产服务器都能直接使用同一个 tag，需要把官方镜像发布成 multi-arch
manifest。

## 变更内容

- .github/workflows/sandbox-runtime-image.yml：新增 QEMU setup，并把发布平台从
  `linux/amd64` 扩展为 `linux/amd64,linux/arm64`。
- scripts/sandbox-runtime-publish.sh：新增 `PLATFORMS` / `VERIFY_PLATFORM` 参数；
  单平台发布保持原有本地 build/selfcheck/push 流程，多平台发布先按单平台自检，再用
  `docker buildx build --push` 推送 multi-arch manifest。
- deploy/sandbox-runtime/README.md：更新 GHCR 发布说明，补充 multi-arch 本地发布命令，
  并说明 `latest` 会按机器架构自动拉取匹配镜像。

## 关键取舍 / 注意事项

- CI selfcheck 仍固定在 `linux/amd64` 上执行一次，避免 multi-arch manifest 无法直接
  load 到 runner 本地镜像库的问题；arm64 运行验证后续可在有原生 arm64 runner 时补充。
- 本地发布脚本默认仍只发布 `linux/amd64`，避免每次手工发布都强制进行较慢的跨架构构建；
  需要 multi-arch 时显式设置 `PLATFORMS=linux/amd64,linux/arm64`。
