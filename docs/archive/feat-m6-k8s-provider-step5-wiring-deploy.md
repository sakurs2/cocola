# M6 Step 5：K8s Provider 接线 + 部署物

## 背景

Steps 1–4 把 K8s+gVisor Provider 的 8 个接口方法、生命周期、Exec/文件 IO、egress
策略全部实现并单测通过。Step 5 按 ADR-0002 铁律完成最后一步:让 backend 可被选中,
并产出可部署到真实集群的清单与 Helm Chart。

## 改动

### 1. 接线(main.go,唯一改动点)

`cmd/sandbox-manager/main.go` 的 `newProvider` switch 增加一行
`case k8s.ProviderName: return k8s.New()`。由 `COCOLA_SANDBOX_PROVIDER=k8s`
选中。service 层 / orchestrator / server 零改动——兑现 ADR-0002"新 backend =
一个新包 + 一处 case"。

### 2. 原始清单 `deploy/k8s/*.yaml`

- `00-namespaces.yaml`:`cocola`(控制面)与 `cocola-sandboxes`(用户沙箱)分离,
  便于把 RBAC/NetworkPolicy 收敛到沙箱命名空间。
- `01-runtimeclass.yaml`:gVisor `runsc` RuntimeClass(handler=runsc)。注明需节点
  预装 `containerd-shim-runsc-v1`。
- `02-rbac.yaml`:`sandbox-manager` ServiceAccount + Role + RoleBinding,最小权限——
  仅在沙箱命名空间对 pods / pvc / configmaps / pods/exec / pods/status /
  networkpolicies 授权。
- `03-sandbox-base.yaml`:共享只读 `cocola-plugins` PVC(挂 `/data/plugins`)+
  命名空间级 default-deny-egress 兜底策略。
- `04-sandbox-manager.yaml`:Deployment(**2 副本**,凭 binding ConfigMap 跨副本
  解析)+ Service;env 用集群 service DNS 替代 host.docker.internal
  (`COCOLA_SANDBOX_LLM_BASE_URL=http://llm-gateway.cocola.svc.cluster.local:8080`)。
- `README.md`:前置条件(gVisor 节点、支持 NetworkPolicy 的 CNI)、apply 步骤、
  对象分布表、休眠/恢复/持久化说明。

### 3. Helm Chart `deploy/helm/cocola-sandbox/`

参数化以上全部对象:namespaces、runtimeClass(可关,适配 GKE 自带 gvisor)、
sandboxManager(镜像/副本/资源)、sandbox(runtimeClass/镜像/storageClass/网关 DNS/
redis/plugins PVC/默认 deny egress)。`values.yaml` 含全部可调项与注释。

## 验证

- `go build ./...`(容器内,`GOWORK=off`、`-mod=mod`)通过,main.go 接线编译成功。
- `gofmt -l cmd/sandbox-manager/main.go` 干净。
- `helm lint deploy/helm/cocola-sandbox`:0 失败(仅 icon 建议)。
- `helm template`:渲染出 10 个对象(Namespace×2、RuntimeClass、ServiceAccount、
  Role、RoleBinding、PVC、NetworkPolicy、Deployment、Service),kind 计数符合预期。
- 原始 `deploy/k8s/*.yaml`:YAML 结构全部通过解析(pyyaml safe_load_all)。

## 已知限制(留待 Step 6 / 后续)

- 本地无 K8s/gVisor 集群,**真实 gVisor compat spike 与端到端 `--resume` 验收
  (Layer C)未做**,标注"待集群"——见 Step 6。
- 域名级 egress 需 DNS-aware CNI(Cilium),原始 NetworkPolicy 仅强制 CIDR/IP
  (见 Step 4)。
- 按沙箱定制 egress 需 orchestrator 传 `Networking`,属 Provider 边界外的后续
  plumbing。

## 不在本步范围

真实集群验收(Step 6,待 gVisor 集群:用户的 Linux 云服务器 / macOS Linux 虚机)。
