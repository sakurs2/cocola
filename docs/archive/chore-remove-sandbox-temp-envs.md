# chore: 移除 sandbox 临时环境变量

- 变更时间：2026-07-06 23:46 (+08:00)

## 变更理由

`make dev` 已定位为默认从 GHCR 拉取 sandbox runtime 镜像的调试入口，不再需要通过临时环境变量切换到本地 k3d registry 推送流程。`sandbox-runtime-verify.sh` 也可以通过是否提供 gateway 环境变量自动决定是否执行 live query，不需要额外的 `SKIP_QUERY` 开关。

## 变更内容

- scripts/run-stack-dev.sh：移除本地 sandbox 镜像 push 分支和 `COCOLA_K8S_PUSH_SANDBOX_IMAGE` 帮助文案，默认只使用 `COCOLA_K8S_SANDBOX_IMAGE_REMOTE`。
- scripts/sandbox-runtime-verify.sh：移除 `SKIP_QUERY` 开关，缺少 gateway env 时自动跳过 live model turn。
- scripts/sandbox-runtime-publish.sh：发布前 selfcheck 不再传入 `SKIP_QUERY`。
- deploy/opensandbox-k8s/README.md、deploy/sandbox-runtime/README.md：删除临时环境变量用法。
