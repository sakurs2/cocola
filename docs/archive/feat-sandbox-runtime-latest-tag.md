# feat: sandbox runtime latest tag

- 变更时间：2026-07-05 23:47 (+08:00)
- 关联提交：待提交

## 变更理由

自部署或临时部署到其他服务器时，希望能直接使用 `latest` 拉取当前最新成功构建的
sandbox runtime 镜像，降低部署命令复杂度。同时仍需要保留 `sha-<commit>` 和
digest-pinned 方式用于可追溯生产发布。

## 变更内容

- .github/workflows/sandbox-runtime-image.yml：默认分支构建时额外发布 `latest` tag。
- scripts/sandbox-runtime-publish.sh：本地发布脚本默认推送 `latest`、`dev` 和 `sha-<commit>`，可用 `PUBLISH_LATEST_TAG=0` 关闭。
- deploy/sandbox-runtime/README.md、.env.example：补充 `latest` 的快速部署用法，并明确它是可变 convenience tag。

## 关键取舍 / 注意事项

- `latest` 用于方便拉取最新镜像，不作为可复现发布标识。
- 生产回滚和审计仍推荐使用 `sha-<commit>` 或 `vX.Y.Z@sha256:<digest>`。
