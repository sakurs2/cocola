# chore: up-k8s use GHCR sandbox runtime

- 变更时间：2026-07-05 23:42 (+08:00)
- 关联提交：待提交

## 变更理由

官方 `cocola-sandbox-runtime` 镜像已经由 GHCR 发布，并支持 multi-arch。`make up-k8s`
本地 Kubernetes 链路继续默认使用 `cocola/sandbox-runtime:dev` 并推送到 k3d local
registry，会要求开发者先本地构建大镜像，也容易残留旧镜像。现在需要让该链路默认直接拉取
远端 package。

## 变更内容

- scripts/run-stack-k8s.sh：默认 `COCOLA_K8S_SANDBOX_IMAGE_REMOTE` 改为
  `ghcr.io/sakurs2/cocola-sandbox-runtime:latest`，`make up-k8s` 默认不再推送本地
  sandbox 镜像；启动时进入 k3d 节点执行 `crictl pull` 预拉远端镜像；保留
  `COCOLA_K8S_PUSH_SANDBOX_IMAGE=1` 作为本地镜像开发开关。
- Makefile：`verify-opensandbox-k8s` 的默认镜像 fallback 同步改为 GHCR `latest`。
- deploy/opensandbox-k8s/README.md：更新 `make up-k8s` 默认拉取远端镜像的说明，并保留
  本地 registry 调试方式。

## 关键取舍 / 注意事项

- k3d cluster 仍保留 local registry 创建逻辑，方便显式本地镜像调试；默认路径不再依赖它。
- 默认预拉远端大镜像会让 `make up-k8s` 启动更久，但避免第一次 Web 对话把冷拉成本压到
  `/sandboxes` 创建链路上并触发 EOF/超时。
- `latest` 是可变 tag，适合本地快速验证；生产环境仍建议使用 digest-pinned 镜像。
