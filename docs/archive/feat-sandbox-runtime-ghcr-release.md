# feat: sandbox runtime GHCR release flow

- 变更时间：2026-07-05 23:18 (+08:00)
- 关联提交：待提交

## 变更理由

沙箱 runtime 镜像已经可以手工推送到 GHCR，但生产使用需要避免只依赖可变 `dev`
tag。需要把发布流程固化到 CI，并提供 commit SHA / semver tag 与 digest-pinned
部署方式，减少人工发布和 tag 漂移带来的不确定性。

## 变更内容

- .github/workflows/sandbox-runtime-image.yml：新增 GHCR 发布 workflow，默认发布 `dev`、`sha-<commit>`、`v*`/semver 和可选手工 tag，并在推送后执行 `--selfcheck`。
- scripts/sandbox-runtime-publish.sh：新增本地发布脚本，支持 build、自检、推送 `dev`、`sha-<commit>` 和可选 `VERSION_TAG`。
- deploy/helm/cocola-sandbox：新增 `sandbox.imageDigest` / `imageWarmer.imageDigest`，生产可把 sandbox runtime 镜像 pin 到 digest。
- deploy/sandbox-runtime/README.md、.env.example：补充 GHCR 发布、权限和 digest-pinned 部署说明。

## 关键取舍 / 注意事项

- 开发默认仍保留 `cocola/sandbox-runtime:dev`，不打断本地 k3d / Docker 工作流。
- 生产推荐使用 `ghcr.io/<owner>/cocola-sandbox-runtime:sha-<commit>@sha256:<digest>` 或 Helm 的 `image + imageDigest` 组合。
- GHCR package 可见性和拉取凭据仍由部署环境管理；公开拉取需要把 package 设为 public，私有拉取则配置 imagePullSecret。
