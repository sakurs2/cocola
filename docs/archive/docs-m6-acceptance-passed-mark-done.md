# docs(m6):M6 验收(Layer C)在 k3d 跑通,路线图 🚧 -> ✅

## 背景

M6(K8s SandboxProvider)此前 Layer A/B(代码 + 单测 + 部署物静态校验)已完成
并合入,仅剩 Layer C(真实集群端到端)待在集群上人工跑一遍。

用户在 **macOS + k3d** 上按 `docs/runbook/m6-k8s-sandbox-acceptance.md` 完整执行
了 Layer C 验收(userns 自检 / 默认 runc + hostUsers=false / egress 锁定 /
原生 bash+file IO / 删 Pod 重挂 PVC 后 `claude --resume` 续接),**全部通过**。

由于 Provider 经标准 client-go 与 API Server 通信、与发行版无关,k3d 跑通即
等价于在同发行版 k3s 及任意 CNCF 一致性集群(EKS/GKE/AKS)上成立。

## 改动

- `README.md`:路线图 M6 状态 🚧 -> ✅,并补注「Layer C 已在 k3d(本地)跑通,
  发行版无关(k3d/k3s/EKS/GKE/AKS)」。

## 验证

- Layer C 由用户在 k3d 实跑,全部用例通过(见 runbook 各步期望输出)。
- 本次仅改 README 一行状态,无代码/部署/逻辑变更。

## 后续

- 预热资源池(warm pool)仍推迟至 M6+(见 ADR-0008 / docs/plan)。
- 下一里程碑 M8(可观测性与压测)保持 ⏳。
