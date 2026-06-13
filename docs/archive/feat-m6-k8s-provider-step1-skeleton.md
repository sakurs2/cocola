# feat(m6): K8s+gVisor Provider 第一步 —— 依赖与骨架(Create/Destroy + PVC)

M6 把沙箱后端从单机 Docker 扩展到 Kubernetes,每个沙箱以 Pod 形式运行在
gVisor(`RuntimeClass=runsc`)下做强隔离。严格遵循 ADR-0002:新增 backend 只实现
`provider.SandboxProvider` 接口,service/orchestrator/main 零侵入。

本步(Step 1)落地依赖引入与 Provider 骨架,完成创建/销毁与 PVC 供给的最小闭环,
其余方法(Pause/Resume/Health/Exec/文件 IO/egress)在后续步骤补齐。

## 改动

- `apps/sandbox-manager/go.mod`:引入 `k8s.io/client-go`、`k8s.io/api`、
  `k8s.io/apimachinery` 三件套,统一钉在 **v0.33.0**(v0.34+ 要求 go≥1.26,
  与本仓 go 1.25 不兼容;v0.33 是兼容 go 1.25 的最后一条线)。
- 新增 `internal/provider/k8s/k8s.go`:
  - `Provider` 结构 + `New`(in-cluster config 优先,回退
    `COCOLA_K8S_KUBECONFIG`/默认 kubeconfig)+ `WithClientset`/`WithNamespace`
    选项(后者供测试注入 fake clientset)。
  - **持久化模型(ADR-0008 T1b/T2)**:用两个 PVC 取代 Docker 的 bind-mount——
    用户 PVC 同时挂 `/data/userdata/<uid>` 与 `~/.claude`(subPath 隔离,跨会话、
    Destroy 后保留);会话 PVC 挂 `/workspace/<sid>`(跨 hibernate);plugins 只读。
  - `Create`:幂等供给两个 PVC(已存在则复用,这正是用户数据跨会话存活的关键)→
    起常驻 idle Pod(`sleep infinity`)+ `RuntimeClassName=runsc` + 四个 cocola 标签
    + 注入 env + 非 root securityContext(uid/gid/fsGroup=10001,使挂载卷可写)。
    Provider 自 mint `sbx-<uuid>`,`Endpoint=k8s://<ns>/<pod>`。
  - `Destroy`:删 Pod,**保留用户 PVC**(跨会话持久),对齐 Docker "留卷" 语义。
  - `resolve`:缓存快路径 → label-selector List 兜底,任意副本可操作任意沙箱
    (对齐 Docker `resolve` 的跨副本契约)。
  - 资源换算 `resourceReqs`、PVC 命名、`safe()` 标识规范化等 helper。

## 测试

`internal/provider/k8s/k8s_test.go`(新增,fake clientset,无需真集群):

- `Create` 起 Pod 带四标签 + `runsc` + 两 PVC + 注入 env + 非 root SC。
- 复用既有用户 PVC(返回用户只新增会话 PVC,总数仍为 2)。
- `Destroy` 删 Pod 但用户 PVC 存活。
- `safe()` 规范化。

容器内(`golang:1.25-alpine`,`GOWORK=off GOFLAGS=-mod=mod`):
`go build ./...`、`go vet ./internal/provider/k8s/`、`go test ./...` 全绿,
gofmt 干净。

## 现实约束(分层验收)

本地 macOS 无 K8s 集群、无 gVisor,Step 1–5(代码 + 部署物 + fake 单测)可全部
本地完成;真集群 gVisor compat spike 与端到端 `--resume`(Layer C)在带 runsc 的
Linux K8s 上验收(用户有 Linux 云服务器),详见 `docs/plan/m6-k8s-gvisor-provider.md`。
