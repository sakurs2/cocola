# feat: k3s sandbox node admin demo

- 变更时间：2026-07-03 16:02 (+08:00)
- 关联提交：待提交

## 变更理由

cocola 需要在 OpenSandbox Kubernetes runtime / k3s 形态下支持轻量机器运维能力，包括添加机器指引、禁用机器、下线机器、恢复机器、查看资源状态和统一运维语义。v1 不做自研调度器，只包装 Kubernetes 原生 node 操作，并提供一个简单 demo UI。

## 变更内容

- apps/admin-api：新增可选 Kubernetes REST node manager，支持 node list、cordon、uncordon、受控 pod eviction 和 k3s join command 展示。
- apps/web：新增 `/admin/sandbox-nodes` demo UI 和 same-origin admin proxy，前端不直接接触 admin-api 地址或 admin key。
- apps/web/app/admin/sandbox-nodes/page.tsx：优化 Offline 操作反馈，提高确认弹窗层级，执行中显示 loading，已 offline 的节点禁用重复下线操作。
- deploy / scripts：给 web 服务补充 admin-api proxy 所需环境变量。
- scripts/run-stack.sh：封装 Docker Compose 调用，优先使用 `docker compose`，不可用时回退到 `docker-compose`，避免部分本机环境下 `make up` 启动 infra 时错误退化为 `docker -f`。
- apps/admin-api/internal/service/kube_rest.go：修复 k3d/k3s 常见 kubeconfig 中 `- context:` / `- cluster:` 列表项被误判为顶层 section 的解析问题，将 kubeconfig server 中的 `0.0.0.0` 归一为 `127.0.0.1`，并补充 kubeconfig parser 单测。
- apps/admin-api/internal/service/sandbox_nodes.go：为 node 写入 `cocola.dev/sandbox-node-mode` annotation，区分 Kubernetes 底层同为 cordon 的 `disabled` 与 `offline` 运维语义；列表刷新后可稳定显示 `offline` / `offline_pending`。
- 关键取舍：不引入 client-go，不 shell out kubectl；v1 仅扫描配置的 sandbox namespace 和 OpenSandbox sandbox label，不做全集群 drain 或跨节点迁移。
