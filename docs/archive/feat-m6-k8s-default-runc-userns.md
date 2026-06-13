# M6:K8s Provider 默认改为 runc + 用户命名空间,gVisor 降级为可选增强

## 背景

此前 K8s Provider 把 gVisor(`runsc` RuntimeClass)当作**默认且必需**的隔离层。
但 gVisor 需要在**每个节点**手动装 `runsc` + `containerd-shim-runsc-v1`、改
containerd 配置并重启——对"自托管、部署尽量简单、支持 k3s 编排"的目标是明显
摩擦。用户明确要求:需要 k3s 编排,但部署要简单,且问是否有比 gVisor 更省事的
沙箱方案。

调研结论:Kubernetes **用户命名空间**(`hostUsers: false`,把容器 root 映射成
宿主机非特权 uid)自 **1.33 起默认开启**,是纯 Pod spec 字段、**零节点安装**,
k3s 1.35.5 开箱即用。与既有的 egress NetworkPolicy + 非 root uid 10001 + seccomp
+ 资源限额叠加,可构成扎实的纵深防御。因此把默认隔离切到 **runc + 用户命名空间**,
gVisor 保留为**一个配置开关**即可启用的可选增强——完全符合 ADR-0002"隔离层是
配置/字段,不是新后端"的铁律(切 runc↔gVisor↔userns 只动 env/config,
service/orchestrator/main 不变)。

## 改动

### 1. Provider 代码 `apps/sandbox-manager/internal/provider/k8s/k8s.go`

- `COCOLA_K8S_RUNTIME_CLASS` 默认由 `runsc` 改为**空**:为空时**不写**
  `pod.Spec.RuntimeClassName`,回落集群默认 runc。设为 `runsc`(或集群自带的
  gVisor class 名)即开启 gVisor。
- 新增 `hostUsers *bool` 字段 + `parseHostUsers` 解析 `COCOLA_K8S_HOST_USERS`:
  默认 `"false"` -> `*bool=false` 开启用户命名空间;`"true"` -> 关闭;
  其他值(`""`/`"default"`)-> `nil` 交给集群默认。podSpec 仅在非 nil 时设
  `pod.Spec.HostUsers`。
- 包注释、`podSpec` 注释、常量(`defaultRuntimeClass` 改名 `gvisorRuntimeClass`)
  同步更新为"默认 runc + userns,gVisor 可选"的语义。
- Create 与 Resume 仍由 binding 驱动,产出 byte-identical 的 Pod(隔离设置一致,
  hibernate/跨副本恢复后 PVC 文件映射一致)。

### 2. 单测 `apps/sandbox-manager/internal/provider/k8s/k8s_test.go`

- `TestCreate_DefaultRunc_NoRuntimeClass`:默认 Provider 不写 RuntimeClassName。
- `TestCreate_UserNamespacesEnabledByDefault`:默认 `Spec.HostUsers == false`。
- `TestParseHostUsers`:false/true/空/default 等取值映射正确。
- 既有 `TestCreate_StartsPodWithLabelsAndPVCs` / `TestResume_*` 显式设
  `p.runtimeClass = "runsc"` 以继续覆盖 gVisor 分支。

### 3. 部署物

- `deploy/k8s/04-sandbox-manager.yaml`:`COCOLA_K8S_RUNTIME_CLASS` 值改为空 +
  新增 `COCOLA_K8S_HOST_USERS: "false"`,附说明注释。
- `deploy/k8s/01-runtimeclass.yaml`:标注为 **可选(仅 gVisor 需要)**,默认路径
  无需 apply。
- Helm `values.yaml`:`runtimeClass.install` 默认 `false`、`sandbox.runtimeClass`
  默认 `""`、新增 `sandbox.hostUsers: "false"`;`templates/sandbox-manager.yaml`
  注入 `COCOLA_K8S_HOST_USERS`。
- `deploy/k8s/README.md`:重写为"默认 runc + userns、零节点安装",gVisor 移至
  "Optional: gVisor enhancement"小节。

### 4. 文档

- 验收 runbook 由 `m6-gvisor-acceptance.md` 重命名为
  `m6-k8s-sandbox-acceptance.md`:默认路径改为 runc + userns(§2 用
  `/proc/self/uid_map` 验用户命名空间生效,不再依赖 dmesg gVisor 指纹);
  gVisor 安装/注册/compat spike 全部下沉到「附录 A」;前置从"集群 v1.29 + 节点装
  gVisor"改为"集群 v1.33+(k3s 1.35.5)+ 内核 >= 5.19,零隔离安装"。
- README:架构「沙箱」一行与 M6 路线图行更新为"默认 runc + 用户命名空间(零节点
  安装),gVisor 为可选增强"。

## 验证

- `gofmt -l` 干净;`go build ./...`、`go vet ./internal/provider/k8s/...`、
  `go test ./internal/provider/k8s/...` 全部通过(本地用 go1.25 toolchain,
  经 http 形式的 GOPROXY 拉取依赖,go.mod 的 `go 1.25.0` 保持不变)。
- 所有改动的 YAML 经无 Tab 缩进校验通过。
- 切换隔离层仅改 env/Helm values,未触碰 service/orchestrator/main(符合 ADR-0002)。

## 不在本步范围(Layer C 实跑,待用户集群)

- 在 k3s 1.35.5 云服务器上按新 runbook 跑通 §2–§4,全绿后把 README M6 标 ✅。
- gVisor 增强路径(附录 A)的实跑与 compat spike。
- 域名级 egress(需 Cilium 等 DNS-aware CNI)、warm-pool 等仍属后续项。
