# feat: OpenSandbox Kubernetes runtime POC

- 变更时间：2026-07-03 17:45 (+08:00)
- 关联提交：待提交

## 变更理由

cocola 已具备基于 OpenSandbox 的沙箱 provider 和 k3s 节点管理 demo，但真实 sandbox 生命周期仍主要依赖 Docker-runtime OpenSandbox Server。本次补齐本地 k3d/k3s POC 路径，让 cocola 可以连接部署在 Kubernetes runtime 下的 OpenSandbox Server，验证真实 sandbox 创建、执行、文件能力和节点运维 UI。

## 变更内容

- deploy/opensandbox-k8s：新增本地 Helm values、`cocola-plugins` PVC manifest 和 README，明确 k3d/k3s 前置、port-forward、环境变量和验证步骤。
- scripts/opensandbox-k8s.sh：新增 OpenSandbox all-in-one Helm chart 安装、port-forward、状态查看和卸载脚本。
- Makefile：新增 `opensandbox-k8s-up`、`opensandbox-k8s-forward`、`opensandbox-k8s-down`、`opensandbox-k8s-status`、`verify-opensandbox-k8s` targets。
- scripts/run-stack.sh / scripts/start.sh：支持 `COCOLA_OPENSANDBOX_MANAGED=0`，避免 provider=opensandbox 时自动启动 Docker-runtime OpenSandbox Server，从而允许连接外部 Helm 管理的 OpenSandbox Kubernetes runtime。
- deploy/opensandbox-k8s/values.local.yaml：覆盖 `controller.snapshot.containerdSocketPath=""`，避免 OpenSandbox chart 默认渲染当前 v0.2.0 controller 镜像不支持的 `--containerd-socket-path` 参数。
- scripts/docker-compose.sh / Makefile / scripts/start.sh：统一 Docker Compose 调用入口，优先使用 `docker compose`，不可用时回退 `docker-compose`，避免部分环境下 `make opensandbox-down` 报 `unknown shorthand flag: 'f' in -f`。
- apps/sandbox-manager：OpenSandbox provider lifecycle HTTP timeout 默认提升到 90s，并支持 `COCOLA_OPENSANDBOX_HTTP_TIMEOUT` 覆盖，避免 Kubernetes runtime 创建 sandbox 最多等待 60s 时被 cocola 30s 客户端超时提前截断。
- deploy/opensandbox-k8s/README.md：补充 k3d 多节点镜像导入要求和 `no space left on device` 排查提示。
- 关键取舍：v1 先验证 Create / Exec / File / Destroy / PVC persistence；Pause / Resume 依赖 snapshot registry，后续单独补齐。
