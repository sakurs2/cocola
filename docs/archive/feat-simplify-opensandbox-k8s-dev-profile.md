# feat: 简化 OpenSandbox Kubernetes runtime 本地验证

- 变更时间：2026-07-03 20:44 (+08:00)

## 变更理由

OpenSandbox Kubernetes runtime 的手动验证流程需要串联 k3d、Helm、kubectl
port-forward、环境变量和 sandbox runtime 镜像导入。这个过程容易出错，尤其是
`k3d image import` 会把大体积 sandbox 镜像导入到每个节点，失败时还可能留下较大的
containerd 数据占用。用户希望获得类似 `make up` 的一键体验。

## 变更内容

- Makefile：新增 `up-k8s`、`down-k8s`、`reset-k8s`、`status-k8s`，并让
  `verify-opensandbox-k8s` 默认使用本地 registry 镜像；移除旧的手动
  `opensandbox-k8s-*` target，保留现有 `make up` 默认路径不变。
- scripts/run-stack-k8s.sh：新增 K8s dev profile 封装，自动创建/复用单节点
  k3d、创建本地 registry、push sandbox runtime 镜像、安装 OpenSandbox K8s
  runtime、启动 port-forward，并以 K8s runtime 环境变量启动 `scripts/run-stack.sh`；退出时
  自动清理 port-forward，`down-k8s` 可按 pid 停止仍在运行的 dev stack。
- scripts/opensandbox-k8s.sh：删除旧的手动分步脚本，其安装、卸载、状态查看和
  port-forward 能力已合并到 `scripts/run-stack-k8s.sh`，避免两套入口并存。
- deploy/opensandbox-k8s/README.md：补充一键启动、状态查看、停止和重置流程，并说明
  本地 registry 取代 `k3d image import` 的原因。
- .env：移除长期写死的 K8s OpenSandbox URL / node selector / timeout 等变量，避免
  普通 `make up` 被外部 port-forward 配置污染；K8s 模式由 `make up-k8s` 临时注入。
