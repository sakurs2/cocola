# M6 Step 6：验收门(Layer C)收尾 + 路线图

## 背景

Steps 1–5 已把 K8s+gVisor Provider 的 Layer A(代码 + 全量 fake-clientset 单测)
与 Layer B(部署物:原始清单 + Helm Chart + 静态校验)在本地全部完成并合入。
按 Plan 的三层拆分,**Layer C(需真集群的 gVisor 端到端验收)在本地无环境,
标注 "待集群" 收尾**。本步产出可在用户的 Linux 云服务器 / macOS Linux 虚机上
直接照跑的验收 runbook,并把 README 路线图 M6 行从 ⏳ 推进到 🚧。

## 改动

### 1. 验收 runbook `docs/runbook/m6-gvisor-acceptance.md`

可执行的 Layer C 验收手册,逐步给出命令与期望输出:

- **§0 前置 + 三选一搭集群**:k3s+gVisor(单机,最贴生产)/ minikube gvisor 插件 /
  GKE Sandbox;含 CNI 强制 NetworkPolicy、gVisor handler 名匹配等卡点提示。
- **§1 部署沙箱平面**:原始清单与 Helm 两种 apply,以及就绪检查。
- **§2 gVisor compat spike**:用 dmesg "gVisor" 指纹确认 runsc 真生效;
  `claude --version` 在 runsc 下可跑(暴露被拦 syscall)。
- **§3 端到端四验**:Pod `runtimeClassName=runsc` / 经 service DNS 打到网关的
  真实 query / egress 出公网被拒 / 原生 bash 与 Exec·WriteFile·ReadFile。
- **§4 持久化续接**(M6 最关键验收):删 Pod 留 PVC+binding -> Resume 重建重挂 ->
  workspace 文件续上 -> `claude --resume` 接回会话;附跨副本验证。
- **§5 验收判定清单**(逐条勾,全绿后 README M6 -> ✅)。
- **§6 排障速查表**。

### 2. README 路线图

M6 行 ⏳ -> 🚧,描述补全为:client-go 实现 8 方法 + 休眠(删 Pod 留 PVC)/
恢复(凭 binding 重建)/Exec 自愈 + egress NetworkPolicy + 部署物(K8s 清单 /
Helm Chart);代码与单测就绪,真实 gVisor 集群端到端验收(Layer C)待集群。

## 验证

- runbook 与本仓实际资源命名一致:Pod `cocola-<sid>`、binding
  `cocola-bind-<sid>`、egress `cocola-egress-<sid>`、命名空间
  `cocola` / `cocola-sandboxes`、env `COCOLA_SANDBOX_LLM_BASE_URL`,
  与 `deploy/k8s/*` 及 provider 实现对齐。
- 不引入代码改动,Layer A/B 既有测试与构建不受影响。

## 不在本步范围(Layer C 实跑,待用户集群)

- 在真 gVisor 集群上按本 runbook 跑通 §2–§4,全绿后把 README M6 标 ✅。
- 若 compat spike 暴露被 runsc 拦截的 syscall,按记录评估基础镜像 / runsc 版本。
- 域名级 egress(需 Cilium 等 DNS-aware CNI)、per-沙箱 egress 的 orchestrator
  plumbing、Vault/MinIO/warm-pool 等仍属 M6 之外的后续项。
