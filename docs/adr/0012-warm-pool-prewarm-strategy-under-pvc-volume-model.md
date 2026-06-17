# ADR-0012: warm pool 在 PVC/bind-mount 卷模型下的预热策略修订

- Status: Accepted
- Date: 2026-06-17
- Deciders: @cocola-maintainers
- Amends: ADR-0008 §3「Warm pool」
- Depends on: ADR-0002（SandboxProvider 抽象铁律）、ADR-0008（持久化分层与 K8s/gVisor 后端）

## Context

ADR-0008 §3 把 warm pool 描述为「idle pool → **bind on demand** → return/destroy
→ async refill」:后台维持一批预热好的沙箱,新会话来时**领用其中一个并绑定到该
用户/会话**。#13 落地引擎(commit `3335c6d` / `6238409`)时,这个「领用 + 绑定」
被实现成可选能力 `provider.Adopter`——把一个 user-agnostic 的预热箱**后挂上该用户
的卷 / 注入会话身份**。引擎本身 backend 无关、`SandboxProvider` 核心接口零改动
(ADR-0002),这部分是对的。

问题出在「后挂卷」这个动作本身,在我们现有的两个 backend 上都不成立:

- **Docker**:bind-mount(userDir / sessDir / pluginDir / claudeDir)固定在
  `ContainerCreate` 时写入 `HostConfig.Mounts`,**运行中的容器无法再挂载新卷**。
  这是 #13 已确认并据此决定「Docker 不实现 Adopter」的原因。
- **K8s**:Pod 的 `volumes` / `volumeMounts`(user PVC、session PVC)写死在
  `Create` 时的 Pod spec 里。Kubernetes 中 Pod spec 的绝大多数字段(含 volumes)
  **创建后不可变**,只有 image / activeDeadlineSeconds / tolerations 等极少数字段
  可改[[Understanding and Working with Immutable Fields in Kubernetes]](https://hoop.dev/blog/understanding-and-working-with-immutable-fields-in-kubernetes)。
  现有 `k8s.go` 的 hibernate/resume 正是「删 Pod → 用同 spec 重建并重挂同一对
  PVC」,而非给运行中的 Pod 热挂卷。

也就是说:**adopt-by-remount(领用预热箱再后挂用户卷)在 PVC/bind-mount 这一类
卷模型上根本无法实现**。原 ADR-0008 §3 的「bind on demand」措辞,隐含了一个我们
现有两个 backend 都不具备的能力。

需要澄清的另一点:**K8s + gVisor 的冷启动成本大头并不在挂卷**。挂 PVC 很便宜,
真正的重头是 ① 1.9GB Route-A 镜像的拉取、② Pod 调度、③ runsc 沙箱初始化、
④ 进程(Node + Claude Code)启动。其中**镜像拉取是最大且最容易单独消除的一项**。

## Decision

**放弃「预热整箱 + 领用时后挂用户卷」(adopt-by-remount)作为 warm pool 的主路径,
改为「节点级镜像预拉 + 运行时预热」。** 具体:

1. **`provider.Adopter` 缝口保留,但承认当前无 backend 实现它。** 它在 ADR-0002
   下是干净的可选能力隔离;binder 的 `tryAdopt` 在「无 backend 实现 Adopter」时
   静默降级到正常 cold Create——这正是 #13 已落地的、永不制造新失败模式的行为。
   保留它是为未来可能出现的、支持卷热挂的 backend(如某些 MicroVM / E2B 形态)
   留口,而**不再把它当作 K8s 的落地路径**。

2. **K8s warm pool 的主路径改为「DaemonSet 镜像预拉(node image warmer)」。** 用一个
   在每个候选节点上跑 `sleep`/`pause` 的 DaemonSet,把 1.9GB Route-A 镜像预拉到
   节点本地镜像缓存——这是社区成熟模式(che `kubernetes-image-puller`、
   `mattmoor/warm-image` 等均以 DaemonSet 在每节点缓存镜像)[[che-incubator/kubernetes-image-puller]](https://github.com/che-incubator/kubernetes-image-puller)。
   这样新会话的 Pod 调度到任意预热节点时,镜像已在本地,`Create` 直接跳过最大的
   那段冷启动,**完全不需要 adopt、不碰卷模型、不动 `SandboxProvider` 接口**。

3. **「预热整箱」退化为可选的二阶优化,且仅在卷模型允许时才考虑。** 即便要预热
   整箱,也应是「按 (user, session) 预测性预创建」而非「user-agnostic 箱后挂卷」,
   这超出当前里程碑范围,留待数据驱动(见 #15 的冷启动实测)再评估。

4. **修订 ADR-0008 §3 的措辞**:把「bind on demand」明确为「**node image warming
   (DaemonSet) 为主路径;adopt-by-remount 需 backend 支持卷热挂,当前 Docker/K8s
   均不支持**」,并指向本 ADR。

## Alternatives Considered

- **A. 照原计划为 K8s 实现 `Adopter`(预热 user-agnostic Pod,领用时热挂 user
  PVC)。** 否决:Pod spec volumes 创建后不可变,Kubernetes 不支持给运行中的 Pod
  热挂 PVC[[Understanding and Working with Immutable Fields in Kubernetes]](https://hoop.dev/blog/understanding-and-working-with-immutable-fields-in-kubernetes)。
  与 Docker 撞同一堵墙,无法实现。

- **B. 预热箱挂 userdata 根目录,领用时按 uid 进子目录。** 否决:把全体用户数据
  根挂进一个 user-agnostic 箱 = 数据越权 / 跨用户泄漏,违反 ADR-0008 的 per-user
  卷隔离。

- **C. 预热箱起好后,领用时用 `cp` / rsync 把用户数据拷进去。** 否决:重造持久化
  轮子,且破坏 ADR-0008 的「hibernate=留卷、resume=重挂同卷」语义(拷贝出来的
  副本与原 PVC 脱钩,写回一致性无解)。

- **D. 用 StatefulSet / 动态 patch 卷。** 否决:StatefulSet 的
  `volumeClaimTemplates` 同样不可变[[How to Handle Immutable Fields in ArgoCD]](https://oneuptime.com/blog/post/2026-02-26-how-to-handle-immutable-fields-in-argocd/view);
  且引入 StatefulSet 会与现有 Pod-per-sandbox 模型冲突。

- **E. 节点镜像预拉(选定方案)。** 直接打击冷启动最大头(镜像拉取),零接口改动、
  零卷模型耦合,是 K8s 社区处理「大镜像冷启动」的标准做法[[che-incubator/kubernetes-image-puller]](https://github.com/che-incubator/kubernetes-image-puller)。

## Consequences

- **Positive**
  - 冷启动优化方向落在最大、最可控的成本项(1.9GB 镜像拉取)上,且不依赖任何
    backend 的卷热挂能力。
  - `SandboxProvider` 核心接口、`provider.Adopter` 缝口、#13 已落地的引擎与降级
    逻辑全部无需改动——本 ADR 是策略澄清,不是代码回退。
  - 避免 #15 在 K8s 上重蹈 Docker 的「后挂卷」覆辙。

- **Negative / 接受的代价**
  - 节点预拉只消除镜像拉取,不消除 Pod 调度 + runsc 初始化 + 进程启动那几秒;
    要进一步压低需要「整箱预热」,而那受限于卷模型,留待数据驱动评估。
  - `provider.Adopter` 成为「已定义但暂无实现」的缝口;需在代码注释与本 ADR 中
    讲清它的前置条件(backend 支持卷热挂),以免后人误以为它是 K8s 的现成路径。

- **Followups**
  - #15:K8s warm pool 落地 = DaemonSet 镜像预拉清单(Layer A/B 本机可做静态校验)
    + runsc 下冷启动实测(对照 #14 EchoProvider 基线量化收益,Layer C 待真集群)。
  - 修订 ADR-0008 §3 的 warm pool 段落,指向本 ADR。
  - 更新 README 路线图,补上 warm pool(#13)与 #15 的位置。
