# docs(m6):K8s 部署/验收文档改为发行版无关 + 新增 k3d 本地路径

## 背景

用户计划未来实现「沙箱集群管理」,但希望先在 **macOS** 本地试,且要「全平台
统一」的方案,而非硬绑 k3s(k3s 仅 Linux,Mac 上需 Linux VM)。

事实依据:`apps/sandbox-manager/internal/provider/k8s/k8s.go` 用标准 client-go
(`rest.InClusterConfig()` 回落 `~/.kube/config`),只与 Kubernetes API Server
通信,**与发行版完全无关**——换发行版只是换 kubeconfig,Provider 零改动。
因此「全平台统一」无需改代码,只需放宽部署/验收文档措辞并补本地路径。

## 改动

仅文档/部署层,**不碰任何 Go 代码 / 部署 YAML 逻辑**。

- `deploy/k8s/README.md`
  - 前置从「集群 v1.33+(k3s 1.35.5)」放宽为「任意 CNCF 一致性 K8s v1.33+」,
    点明 Provider 经 client-go 与发行版无关:dev 用 k3d/kind/minikube/OrbStack/
    Docker Desktop,prod 用 k3s 或 EKS/GKE/AKS,同一套 manifests。
  - 补 macOS 注记:K8s 节点跑在 Linux VM 中,>= 5.19 内核由该 VM 满足。
  - 新增「Local development on macOS/Linux (k3d, recommended)」章节:k3d 三步
    起集群(create / build+`k3d image import` / apply),并说明 k3d 跑的就是
    同一个 k3s 发行版,以及为何 Mac 上不直接裸装 k3s。

- `docs/runbook/m6-k8s-sandbox-acceptance.md`
  - 适用对象从「Linux 云服务器 + k3s」放宽为「任意 CNCF 一致性 K8s v1.33+」,
    prod 首选 k3s、dev 首选 k3d,同套 manifests 两边通用。
  - 新增「本地开发:macOS / Linux 用 k3d」小节(起集群 + `k3d image import` +
    macOS 内核/VM 注记),置于原「Linux 云服务器搭 k3s」之前。
  - §2a 的 `/proc/self/uid_map` 自检补一句:macOS/k3d 下该步同时验证承载节点
    的 Linux VM 内核 userns 已就绪(即「验 hostUsers 生效」这步对 Mac 已覆盖)。

## 验证

- `git diff --stat`:仅上述 2 个文档文件改动(+69/−6)。
- 两文件均无 tab(prettier 友好),无 Go/YAML/逻辑改动,不影响构建与单测。

## 不在本次范围

- 预热资源池(warm pool):用户确认推迟,留作 M6+(见 docs/plan & ADR-0008)。
- 在 Mac 上实跑 k3d 端到端:由用户本地执行,本次只交付文档与命令。
