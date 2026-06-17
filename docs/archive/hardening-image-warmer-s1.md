# 变更归档:#15 S1 — image-warmer DaemonSet(K8s 节点镜像预拉)

- 任务:#15 gVisor 兼容性 spike + K8s warm-pool 节点镜像预拉
- 阶段:S1(节点镜像预热的清单与 Helm 模板落地 + 静态校验)
- 关联:ADR-0012(warm pool 在 PVC/bind-mount 模型下的预热策略)、ADR-0008 §3、Plan `docs/plan/hardening-gvisor-spike-and-image-warmer.md` §4.1

## 背景

ADR-0012 已收口:warm pool 的「adopt + remount」在两套后端上都不可实现(Docker bind-mount 在 ContainerCreate 固定;K8s Pod 卷在创建后不可变)。在 K8s 上,Route-A 冷启动最大的开销不是挂卷,而是把约 1.9 GB 的 sandbox 运行时镜像(Node + Claude Code)拉到落点节点。因此 K8s 的预热路径转向「让镜像始终在每个候选节点上」:新 sandbox Pod 调度时直接命中节点镜像缓存,跳过拉取。

## 本阶段改动

新增 / 修改三个文件,均为**默认关闭**、零节点级安装的可选能力:

1. `deploy/k8s/07-image-warmer.yaml`(新增,raw 清单)
   - DaemonSet `cocola-image-warmer`,namespace `cocola-sandboxes`。
   - `initContainers[0]` 引用 sandbox 镜像、`command: ["true"]`:调度即触发节点本地拉取,随即退出 0。
   - 主容器 `pause`(`registry.k8s.io/pause:3.9`)常驻,保持 Pod Running,把已拉镜像钉在节点镜像缓存中(社区通用预拉模式,如 kubernetes-image-puller / warm-image)。
   - `terminationGracePeriodSeconds: 1`、`automountServiceAccountToken: false`;`nodeSelector`/`tolerations` 以注释形式给出范围化示例。
   - 头部注释解释 WHY(1.9 GB 镜像 + ADR-0012)、HOW(`["true"]` initContainer 触发拉取 + pause 钉缓存)、egress(镜像拉取走 kubelet/containerd 而非 Pod 网络,故 cocola-sandboxes 的 default-deny-egress NetworkPolicy 不会阻断预热)、OPTIONAL(Helm 默认关闭)。

2. `deploy/helm/cocola-sandbox/templates/image-warmer.yaml`(新增,Helm 模板)
   - 由 `{{- if .Values.imageWarmer.enabled }}` 守卫,默认不渲染。
   - 标签显式书写,**不**复用 `cocola-sandbox.labels` helper —— 后者会注入 `app.kubernetes.io/name: cocola-sandbox`,与本资源所需的 `cocola-image-warmer` 冲突,造成 metadata/selector/template 三处 `app.kubernetes.io/name` 重复键、selector 与实际 Pod 标签不匹配。改为显式标签后三处统一为 `cocola-image-warmer`。
   - 镜像 `{{ .Values.imageWarmer.image | default .Values.sandbox.image }}`:留空即回退 `sandbox.image`,避免与 provider 实际镜像漂移。
   - 资源、`pauseImage`、`nodeSelector`、`tolerations` 全部参数化。

3. `deploy/helm/cocola-sandbox/values.yaml`(修改)
   - 追加 `imageWarmer:` 配置块:`enabled: false`、`image: ""`(回退 sandbox.image)、`imagePullPolicy`、`pauseImage`、`nodeSelector`、`tolerations`、`initResources`、`pauseResources`。

## 复用,不造轮子

- 复用既有 chart 约定与 `sandbox.image` 变量,镜像单一事实来源。
- 预拉采用社区成熟模式(initContainer 触发拉取 + pause 钉缓存),不引入任何第三方 operator,符合「零节点级安装」默认。

## 静态校验(无集群)

- `helm template`:默认(`imageWarmer.enabled=false`)渲染 **0** 个 image-warmer 对象;`--set imageWarmer.enabled=true` 渲染出 DaemonSet。
- 标签去重核对:渲染后 metadata.labels / spec.selector.matchLabels / spec.template.metadata.labels 三处 `app.kubernetes.io/name` 均为 `cocola-image-warmer`,selector ⊆ template labels。
- 镜像回退核对:未设 `imageWarmer.image` 时 initContainer 正确取到 `cocola/sandbox-runtime:dev`。
- 说明:沙箱环境无网络/无集群,kubeconform 与 `kubectl --dry-run` 的 schema 校验(需 openapi)不可用;以 helm 渲染 + 结构断言作为本阶段静态验收,真实集群 schema 校验留待 S4 在目标集群执行。

## 不在本阶段(后续)

- S2:`verify-gvisor.sh`(runsc 兼容性探针 1–6,含 checkpoint/restore 探针)+ 冷启动复测脚本(dry-run)。
- S3:若 runsc 兼容性暴露问题,做条件化 provider 修正。
- S4:真实集群验收(DaemonSet 实际预拉效果、冷启动复测、kubeconform/dry-run schema 校验)。
