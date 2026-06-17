# Plan: #15 gVisor(runsc)兼容性验收门 + K8s warm-pool 节点镜像预拉

> 状态:规划中(2026-06-17)。本文是落地方案,先于编码。
> 关联:ADR-0009(runtime 进沙箱)、ADR-0008 §3(warm pool)、ADR-0012(warm
> pool 在 PVC/bind-mount 模型下改走节点镜像预拉)、ADR-0002(SandboxProvider
> 铁律)。前置数据:#14 容量基线(`bench/README.md` §3.2)。

## 1. 目标与动机

本任务收尾两件互相咬合的事,它们都卡在「需要带 gVisor 的真 K8s 集群」这道门上:

1. **gVisor(runsc)兼容性验收**:验证 Route A 大脑(Node + Claude Code)在
   `RuntimeClass=runsc` 下能完整跑通——这是 ADR-0008/0009 定的「先在 runc 跑通、
   再换 runtime」的 pre-prod 验收门,而非前置阻塞。runsc 用用户态内核拦截 syscall,
   对 Node 这类重 runtime 偶有兼容坑(如某些 `io_uring`/特殊 syscall),必须实测。
2. **K8s warm-pool 落地**:按 ADR-0012,K8s 的预热不走 adopt-by-remount(卷模型
   不支持热挂),改走**节点镜像预拉(DaemonSet image warmer)**,把 1.9GB Route-A
   镜像的拉取从请求关键路径上挪走,这是 K8s+gVisor 冷启动的最大头。

### 1.1 冷启动数据(动机量化,来自 #14)

| 路径 | 首请求(冷启,建沙箱) | 稳态(复用沙箱) | 冷启净增量 |
| --- | --- | --- | --- |
| 复用同 session ×6 | 1.69s(#1) | ~0.62–0.67s(#2–6) | — |
| 每次新 session ×6 | ~1.0–1.16s(均值 ~1.08s) | — | **≈ 0.44s/请求** vs 复用 ~0.64s |

> 以上是 Docker EchoProvider 基线。gVisor 冷启更重(镜像 1.9GB + runsc 初始化),
> 真实增量需在目标集群复测——正是本任务 Layer C 的产出。

## 2. 复用不造轮子原则

- **RuntimeClass**:`deploy/k8s/01-runtimeclass.yaml` 与 Helm
  `templates/runtimeclass.yaml` 已就绪,本任务**不新增 runsc 装配**,只补「如何在
  目标集群启用 + 验收」的脚本与文档。
- **镜像预拉**:采用 K8s 社区成熟的 **DaemonSet 预拉模式**(che
  `kubernetes-image-puller`、`mattmoor/warm-image` 均以「每节点跑一个长睡 Pod、
  把目标镜像钉在本地缓存」实现)[[che-incubator/kubernetes-image-puller]](https://github.com/che-incubator/kubernetes-image-puller),
  cocola 只写一份最小 DaemonSet 清单复用此模式,**不引第三方 operator/CRD**。
- **镜像变量**:预拉清单复用 Helm 既有的 `sandbox.image`
  (`deploy/helm/cocola-sandbox/values.yaml`),与 provider 实际拉取的镜像同源,
  避免漂移。
- **压测**:复用 #14 的 `bench/`(k6 SSE / ghz gRPC),冷启动复测沿用 §3.2 的
  「逐请求计时、新 session 触发建箱」方法,不另造脚本。

## 3. 现状勘定

| 资产 | 位置 | 本任务关系 |
| --- | --- | --- |
| runsc RuntimeClass | `deploy/k8s/01-runtimeclass.yaml`、helm `templates/runtimeclass.yaml` | 已就绪,验收时启用 |
| K8s Provider(8 方法 + PVC + Pod spec) | `apps/sandbox-manager/internal/provider/k8s/k8s.go` | 已就绪;`runtimeClass` 字段读 `COCOLA_K8S_RUNTIME_CLASS` |
| plugins PVC + default-deny egress | `deploy/k8s/03-sandbox-base.yaml` | 预拉 DaemonSet 同 namespace 复用 |
| Helm chart | `deploy/helm/cocola-sandbox/` | 新增 image-warmer 模板挂这里 |
| 冷启动基线 | `bench/README.md` §3.2 | Layer C 复测对照 |
| warm pool 引擎 | `internal/orchestrator/warmpool/` | 节点预拉与它正交(预拉降低 Create 成本,引擎管 idle 池);本任务不改引擎 |

## 4. 设计

### 4.1 节点镜像预拉 DaemonSet(Layer B 主产出)

新增 `deploy/k8s/07-image-warmer.yaml` + Helm `templates/image-warmer.yaml`:

- **形态**:DaemonSet,`initContainers` 用 `sandbox.image` 跑 `["true"]`(仅触发
  节点拉取),主容器 `pause`/`sleep` 长驻把镜像钉在节点缓存——标准预拉模式。
- **调度**:`nodeSelector` / `tolerations` 对齐沙箱 Pod 的调度域(只在会跑沙箱的
  节点池预拉),避免在无关节点浪费拉取。
- **开关**:Helm `imageWarmer.enabled`(默认 false,与「零节点安装」缺省一致);
  `imageWarmer.image` 缺省回落 `sandbox.image`。
- **与 egress 的关系**:预拉 Pod 不进沙箱 namespace 的 default-deny(它需要拉镜像
  即访问 registry),放在独立 namespace 或显式放行 registry。设计时显式说明。

### 4.2 runsc 验收脚本(Layer C 门,本机只能准备)

新增 `deploy/k8s/verify-gvisor.sh`(或 `bench/gvisor-compat/`):在已装 gVisor 的
集群上跑一组 compat 探针,逐项判定 Route A 在 runsc 下是否健康:

1. `claude --version` / `node --version` 在 runsc Pod 内正常退出(0);
2. 一次真实带 egress 的 query 跑通(经 gateway,验证 NetworkPolicy + runsc 网络栈
   不打架);
3. 原生 bash / file IO(写读 `/workspace`、`~/.claude`)正常;
4. hibernate→resume(删 Pod 留 PVC→重建重挂)后 `claude --resume` 继续会话;
5. 冷启动逐请求计时(对照 §3.2 基线),量化 runsc + 1.9GB 镜像的真实冷启增量,
   以及**开/关节点预拉**两组对照,验证预拉收益。

### 4.3 分层(对齐 M6 的 A/B/C 套路)

- **Layer A(本机可做)**:无新增 Go 代码——K8s provider 的 runsc 装配已就绪。
  若验收发现 runsc 兼容坑需改 provider(如 Pod securityContext / syscall 规避),
  再按 ADR-0002 在 k8s 包内最小改动 + fake clientset 单测。
- **Layer B(本机可做)**:image-warmer DaemonSet 清单 + Helm 模板 + values 开关;
  用 `helm template` / `kubeconform` 静态校验(容器内,不需真集群)。
- **Layer C(待真集群)**:在带 runsc 的 K8s 上跑 4.2 的 compat 脚本 + 冷启动复测;
  结果回填 `bench/README.md` §3.2 与 ADR-0008/0012;本机无环境时标注「待集群」,
  不阻塞 A/B 合并。

## 5. 落地分层(提交计划)

| 阶段 | 内容 | 本机可验收? | 产出 |
| --- | --- | --- | --- |
| S1 | image-warmer DaemonSet 清单 + Helm 模板 + values 开关 | ✅(helm template / kubeconform 静态) | `deploy/k8s/07-*.yaml`、helm 模板、README |
| S2 | runsc compat + 冷启动复测脚本(可本机 dry-run 语法) | ⚠️ 仅 dry-run | `deploy/k8s/verify-gvisor.sh` / `bench/gvisor-compat/` |
| S3 | (条件触发)若 Layer C 暴露 runsc 兼容坑 → provider 最小修 + 单测 | ✅(fake clientset) | k8s.go 改动 + 单测 |
| S4 | Layer C 真集群验收 + 数据回填 ADR/bench | ❌ 待目标集群 | bench §3.2 数据、ADR Status 更新 |

每个 S 阶段独立提交,各带 `docs/archive/` changelog,不带 `.claude/`,不跳 git hooks。

## 6. 风险

- **runsc + Node 兼容坑**:Node 重 runtime 在 gVisor 下偶有 syscall 不支持;属
  本任务要验收的核心未知,Layer C 才能定论。若踩坑,按 S3 在 provider 侧规避。
- **节点预拉只削镜像拉取**:Pod 调度 + runsc 初始化 + 进程启动那几秒仍在;预拉是
  最大头但非全部,真实收益以 Layer C 数据为准(ADR-0012 已记此代价)。
- **本机无 gVisor**:macOS Docker Desktop 跑不了真 runsc,Layer C 必须留到目标
  集群;A/B 不受阻。
- **预拉镜像与 provider 镜像漂移**:用同一 `sandbox.image` 变量消除,S1 即固化。

## 7. 验收标准

- S1:`helm template` 渲染出 image-warmer DaemonSet,`kubeconform` 通过;开关
  默认 false 时不渲染。
- S2:脚本 `bash -n` 语法干净;探针项与 4.2 一一对应。
- S4(待集群):runsc 下 4.2 五项探针全绿;冷启动复测数据回填 bench §3.2;
  开/关预拉两组对照量化预拉收益;ADR-0008/0012 Status 更新为「真集群已验收」。
