# fix: extend OpenSandbox K8s cold-start timeout

- 变更时间：2026-07-04 15:41 (+08:00)

## 变更理由

`make up-k8s` 下 web 对话请求会长时间无响应。排查后发现 agent-runtime 在获取 sandbox 时收到 OpenSandbox 504：Kubernetes sandbox Pod 需要先 provision PVC，再拉取约 2.9GB 的 `cocola/sandbox-runtime:dev` 镜像，冷启动耗时超过 OpenSandbox 默认 60 秒 ready timeout，导致 Pod 被提前删除，前端表现为 `/api/chat` 等待约一分钟后没有有效回复。

## 变更内容

- deploy/opensandbox-k8s/values.local.yaml：为本地 OpenSandbox K8s profile 设置 `sandbox_create_timeout_seconds = 180`。
- scripts/run-stack-k8s.sh：将 `COCOLA_OPENSANDBOX_HTTP_TIMEOUT` 默认值从 90s 调整为 240s，避免 cocola 侧客户端先于 OpenSandbox server 超时。
- apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go：将 OpenSandbox provider 默认 lifecycle HTTP timeout 调整为 240s，并更新注释说明冷启动镜像拉取场景。
- 验证：`bash -n scripts/run-stack-k8s.sh`、`GOWORK=off GOCACHE=/private/tmp/cocola-go-build-cache go test ./internal/provider/opensandbox`、`make verify-opensandbox-k8s`。
