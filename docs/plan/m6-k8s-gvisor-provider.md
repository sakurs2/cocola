# Plan: M6 K8s + gVisor Provider —— 让沙箱跑在 Kubernetes 上、用 gVisor 强隔离

> ⚠️ **SUPERSEDED by ADR-0014(2026-06-28)**:自建 k8s+gVisor 沙箱后端已退役,
> 主力后端改为 OpenSandbox(docker 保留为兜底)。本文件仅作历史记录,不再维护。


> 状态:规划中(2026-06-13)。本文是落地方案,尚未动代码。

## 1. 目标与动机

当前唯一的 `SandboxProvider` 实现是 Docker(`internal/provider/docker`),适合单机
开发与 demo。M6 新增 **K8s 实现**,兑现 cocola "企业级、生产可自托管" 的定位:

- **强隔离**:每个沙箱 Pod 用 `RuntimeClass=runsc`(gVisor),把不可信的用户代码
  (Route A 下大脑+凭据都在容器内,见 ADR-0009)关进用户态内核沙箱。
- **横向调度**:由 K8s 调度成百上千个沙箱 Pod,而非单机 Docker 守护进程。
- **生产持久化形态**:用户/会话数据落 **PVC**(而非 Docker 的 bind-mount),
  hibernate = 删 Pod 留 PVC(scale-to-zero),resume = 新 Pod 重挂同一对 PVC +
  `claude --resume`,落地 ADR-0008 的 T1b/T2 分层。
- **强制 egress allowlist**:用 NetworkPolicy 把沙箱出网收敛到仅网关 +
  必需内部服务,兑现 ADR-0009 "egress 锁定是强制项" 的安全前提。

**铁律(ADR-0002):** 新 Provider 只实现 `provider.SandboxProvider` 接口,
service 层 / `cmd/sandbox-manager/main.go` / orchestrator 零改动。新增一个 backend =
一个新包 + 一处 `case`(或 `init()` 里 `Register`)。

## 2. 现实约束与分层策略(重要)

本地是 macOS,`kubectl` 已装但**无任何集群上下文**,且 kind/minikube/k3d/helm
均未安装;macOS 上 Docker Desktop **跑不了真正的 gVisor**(runsc 是 Linux 内核态)。

这与 ADR-0008 的判断一致:**gVisor cutover 是"先在 runc 上跑通 Route A 链路、
再换 `--runtime=runsc`"的验收门,而非前置阻塞**;因为 runsc 只是换
`--runtime`/`RuntimeClass`,runc 链路可原样复用。

因此 M6 拆成三层,前两层在本地可全部完成并测试,第三层是需要真集群的验收门:

- **Layer A(本地可完成,代码主体)**:用 `client-go` 写 K8s Provider 包,
  用 `fake` clientset 做全量单元测试 + binder 契约测试。**不需要真集群。**
- **Layer B(本地可完成,部署物)**:`deploy/k8s` 原生 YAML + `deploy/helm` Chart;
  用 `helm template` / `kubeval`(容器内)做静态校验。**不需要真集群。**
- **Layer C(验收门,需真集群,可延后)**:在一套带 gVisor RuntimeClass 的 K8s
  上跑 compat spike(`claude --version`、一次真实带 egress 的 query、原生
  bash/file IO、重挂卷 `--resume`)。本地无环境时,本层标注为 "待集群" 并在
  changelog 注明,不阻塞 A/B 合并。

## 3. 范围

### 做(本里程碑)

1. **K8s Provider 包** `apps/sandbox-manager/internal/provider/k8s/`:用 `client-go`
   实现 8 个接口方法,行为对齐 Docker 参考实现(见 §4)。
2. **跨副本解析**:Pod 打四个 cocola 标签,`resolve` 用 label-selector List,
   保证任意 sandbox-manager 副本能 Pause/Resume/Destroy/Health 任意 Pod
   (对齐 Docker `resolve` 的 cache+label 回退模式)。
3. **卷模型(PVC)**:`SessionID` → 会话 PVC(以 subPath 挂 `/workspace` 与
   `/home/cocola/.claude`),plugins 只读。映射关系与 Docker bind-mount 完全一致,只换后端。
4. **生命周期**:`Pause` = 删 Pod 留 session PVC;`Resume` = 用同 spec 重建 Pod 重挂
   PVC;`Destroy` = 删 Pod,session PVC 由 cocola 生命周期管理清理。
5. **Exec**:走 Pod exec 子资源(stdio,SPDY/websocket),分离 stdout/stderr,
   支持 stdin、超时、退出码——事件协议与 Docker 完全一致(含 channel 关闭语义)。
   "Resume 后再 exec" 的自愈:若 Pod 不存在(被 Pause 删掉)则先重建再 exec,
   对齐 Docker 的 `thawIfPaused`。
6. **WriteFile/ReadFile**:经 exec 用 tar 流写/读(`kubectl cp` 等价实现)。
7. **egress 强制**:Create 时按 `Networking.EgressAllowlist` 生成/绑定
   NetworkPolicy;空 allowlist = 拒绝所有出网(对齐 Docker 的 `NetworkMode=none`),
   非空 = 仅放行清单(至少含网关)。
8. **wiring**:`main.go` 加 `case "k8s"` 或 `init()` 里 `Register`,由
   `COCOLA_SANDBOX_PROVIDER=k8s` 选中。新增 K8s 相关配置 env(namespace、
   StorageClass、RuntimeClass、镜像、网关 DNS 等)。
9. **部署物**:`deploy/k8s/*.yaml`(全套服务 + RuntimeClass + RBAC)与
   `deploy/helm` Chart;`COCOLA_SANDBOX_LLM_BASE_URL` 改用集群 service DNS
   (`http://llm-gateway.<ns>.svc.cluster.local:<port>`)替代 host.docker.internal。
10. **测试**:fake clientset 单测(覆盖 8 方法 + resolve + egress 生成);
    binder 契约场景对齐 `binder_test.go`;部署物静态校验。

### 不做(后续 / 验收门)

- **真集群上的 gVisor compat spike(Layer C)**:本地无 K8s/gVisor,标注 "待集群"。
- **Vault 密钥托管(T3)**:ADR-0008 留待后续,M6 仍用 env 注入凭据。
- **MinIO/对象存储(T2 文件版本)**:后续。
- **warm pool(预热 Pod 池)**:ADR-0008 提到的优化,M6 先做冷启,池化留作 M6+ 优化。
- **orchestrator 改动**:binder 的 Acquire 创建路径目前不传 `Resources`/
  `Networking`;若需要 per-sandbox egress,M6 先用 Provider 配置级默认 allowlist
  兜底(网关 DNS),把"按沙箱定制 egress"列为后续 plumbing(属 orchestrator 改动,
  超出 Provider 边界,本里程碑只标注不实现)。

## 4. 接口逐方法映射(Docker → K8s)

| 方法 | Docker 行为 | K8s 实现 | 必须保持的隐含契约 |
|---|---|---|---|
| Create | 起常驻 idle 容器 + 4 挂载 + 标签 | 起常驻 idle Pod(`sleep infinity`)+ RuntimeClass=runsc + 2 PVC + RO plugins + 4 标签 + 注入 env | Provider 自己 mint `sbx-<uuid>`;Endpoint=`k8s://<ns>/<pod>`;非 root uid 10001 可写 `~/.claude` |
| Exec | docker exec + stdcopy 解复用 | Pod exec 子资源,SPDY 流分离 stdout/stderr | 事件协议一致;stdin/超时/退出码;Pod 不存在则先 Resume 再 exec(自愈) |
| WriteFile/ReadFile | CopyTo/FromContainer(tar) | 经 exec 传 tar | 写到 `dirname(path)`,目录需存在 |
| Pause | ContainerPause(freezer) | 删 Pod,保留两 PVC,meta 记 paused | 廉价且可被后续 Resume 还原 |
| Resume | ContainerUnpause | 用同 spec 重建 Pod 重挂 PVC | 还原后 `claude --resume` 可续接 |
| Destroy | ContainerRemove(留卷) | 删 Pod,**保留用户 PVC** | 不删持久卷(跨会话 userdata) |
| Health | inspect:Running&&!Paused | get Pod:Phase==Running 且 Ready | Healthy/Detail 语义一致 |
| resolve | cache→label 查容器 | cache→label-selector List Pod | 跨副本可操作他人创建的沙箱 |

## 5. 关键技术决策

- **依赖**:引入 `k8s.io/client-go` + `k8s.io/api` + `k8s.io/apimachinery`
  (成熟官方库,符合"复用 OSS"原则)。sandbox-manager 不在 go.work 内,独立
  `go.mod`(go 1.25),容器内构建(`golang:1.25`,`GOWORK=off GOFLAGS=-mod=mod`)。
- **Exec 传输**:用 `client-go` 的 `remotecommand` SPDY executor,不开任何监听端口
  (对齐 ADR-0009 "沙箱永不 bind 端口")。
- **配置 env(新增)**:`COCOLA_K8S_NAMESPACE`、`COCOLA_K8S_STORAGE_CLASS`、
  `COCOLA_K8S_RUNTIME_CLASS`(默认 `runsc`)、`COCOLA_K8S_IMAGE`、
  `COCOLA_K8S_KUBECONFIG`(空=in-cluster ServiceAccount)。Provider 读 in-cluster
  config 优先,回退 kubeconfig。
- **可测试性**:所有 K8s 调用经 `kubernetes.Interface`,单测用
  `k8s.io/client-go/kubernetes/fake` 注入,无需真集群(对齐 docker_test 的
  fake 思路)。exec 这类无法用 fake 覆盖的部分,抽到窄接口 + 用 spike 在 Layer C 验。

## 6. 落地步骤(建议提交粒度)

1. **Step 1 — 依赖与骨架**:加 client-go 依赖;新建 `internal/provider/k8s/k8s.go`
   骨架(struct、New、Register、常量、标签、PVC 命名),`Create`/`Destroy` 先通,
   fake 单测起步。容器内 `go build`+`go test` 绿。changelog + commit。
2. **Step 2 — 生命周期 + resolve**:Pause/Resume/Health + label-selector resolve,
   fake 单测覆盖跨副本场景。commit。
3. **Step 3 — Exec + 文件 IO**:Pod exec 流 + WriteFile/ReadFile + Pod 缺失自愈;
   exec 抽窄接口,可 fake 的逻辑单测。commit。
4. **Step 4 — egress NetworkPolicy**:按 allowlist 生成 NP,空=拒绝全部;单测
   校验生成的 NP 对象。commit。
5. **Step 5 — wiring + 部署物**:main.go case;`deploy/k8s` YAML + `deploy/helm`
   Chart;网关 service DNS;`helm template` 静态校验。commit。
6. **Step 6 — 验收门(Layer C,待集群)**:有 gVisor 集群时跑 compat spike 与
   端到端 `--resume`;无环境则标注 "待集群" 收尾。changelog + commit。

## 7. 验收标准

- Layer A/B:`go build ./... && go vet ./... && go test ./...`(sandbox-manager 全包,
  容器内)全绿;K8s provider 单测覆盖 8 方法 + resolve + egress;`helm template`
  与 YAML 静态校验通过;gofmt 干净;每步一份 `docs/archive/` changelog。
- Layer C(集群就绪后):runsc 下 `claude --version` OK;一次真实 query 经
  service DNS 打到网关并返回;原生 bash/file IO 正常;删 Pod 后重挂 PVC
  `--resume` 能续接上下文。
- README 路线图 M6 行:Layer A/B 合并后标 🚧(进行中,代码就绪/待集群验收),
  Layer C 通过后标 ✅。
