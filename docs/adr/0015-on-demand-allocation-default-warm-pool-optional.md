# ADR-0015: 默认按需冷启动分配,warm pool 降级为可选优化(OpenSandbox-only 语境)

- Status: Accepted（"默认按需分配"仍有效;"warm pool 保留为可选"部分已由 ADR-0016 收敛——能力已移除）
- Date: 2026-06-28
- Deciders: @cocola-maintainers
- Amends: ADR-0008 §3「Warm pool」、ADR-0012(在 OpenSandbox 成为唯一主后端后再次收敛)
- Depends on: ADR-0002(SandboxProvider 可插拔铁律,不变)、ADR-0014(OpenSandbox 定为主力后端、退役 k8s)、ADR-0008(双卷持久化模型)

## Context

ADR-0012 在「Docker + K8s 两后端并存」的语境下,已经否决了「预热整箱 + 领用时后挂用户卷」
(adopt-by-remount)作为 warm pool 主路径,理由是两个后端都无法给运行中的容器/Pod
热挂卷;并把 K8s 的 warm-pool 主路径改为「DaemonSet 节点镜像预拉」。

此后 ADR-0014 把后端收敛为:**OpenSandbox 为唯一主力后端,docker 仅作零配置兜底,k8s
provider 已删除**。这让 ADR-0012 的两个前提都需要重新校准:

1. **「DaemonSet 镜像预拉」是 K8s 专属手段。** k8s provider 已退役,这条主路径在
   OpenSandbox-only 的部署里不再直接适用——镜像缓存由 OpenSandbox server 自身的运行时
   (Docker / 其内建 K8s 运行时)管理,不再由 cocola 用一个 DaemonSet 去铺。

2. **adopt-by-remount 在新主后端上仍然不成立。** 实测正在运行的 OpenSandbox server 的
   OpenAPI:对一个已存在的沙箱,仅暴露 `PATCH metadata`、`pause`、`resume`、
   `renew-expiration`、`snapshots`、`proxy/*` 这些变更端点,**没有任何给运行中沙箱增删卷
   的接口**;`volumes` 只能在 `POST /sandboxes` 创建时一次性指定(见 ADR-0008 双卷映射、
   commit 36326a0)。也就是说,「领一个无身份预热箱再后挂该用户的 PVC」这个动作,在唯一
   主后端上依然无法实现——和当年 Docker/K8s 撞的是同一堵墙。

与此同时,cocola 的目标用户是**自托管的中小并发部署**。binder 的按需路径(快路径续租
复用、慢路径加锁后 `Create` 并在创建时挂双卷)已在真 server 上验收通过,冷启动的那几秒
对这类部署完全可接受。继续把 warm pool 摆在「默认开启的主路径」位置,只会带来一个
**默认开了却在空转**(领用 → 无 Adopter → 销毁 → 降级 cold create)的迷惑态。

需要明确划定的边界:本 ADR 不回退任何已落地代码,只调整「默认走哪条路」这一策略表态。

## Decision

**把「按需冷启动分配」确立为 cocola 沙箱分配的默认且唯一主路径;warm pool 降级为
默认关闭的可选高并发优化。** 具体:

1. **默认按需分配。** 不设 `COCOLA_WARMPOOL_ENABLED` 时(默认),binder 走:
   快路径(已有绑定 → 续租复用)/ 慢路径(加锁 → 双检 → `provider.Create` 并在创建时
   映射双卷)。这条路径无任何架构妥协:卷在 create 时一次性挂好,与 OpenSandbox API
   契约一致。

2. **warm pool 保留为可选,默认关闭。** `warmpool.Pool`、binder 的 `tryAdopt`、
   `provider.Adopter` 缝口、相关单测全部**原地保留,不删**。它作为「未来出现支持卷热挂的
   backend(某些 MicroVM / E2B 形态)时」的预留口。在当前 OpenSandbox 主后端下,
   即便开启也因无 Adopter 实现而静默降级到 cold create——这是 #13 已落地的、永不制造
   新失败模式的行为。

3. **承认并文档化「warm pool 在当前主后端无收益」。** OpenSandbox 不支持运行中热挂卷,
   故 adopt-by-remount 不可用;若未来要为 OpenSandbox 提供真正的预热收益,正确方向是
   **「按 (user, session) 预测性预创建带身份的箱」**(预热时就把用户卷挂上,而非建无身份
   箱再领用),这超出当前范围,需另起 Plan/ADR、由冷启动实测数据驱动。

4. **main.go 启动告警增强。** 当 `COCOLA_WARMPOOL_ENABLED` 开启但 provider 未实现
   Adopter 时,日志明确提示「warm pool 将空转、不会带来收益,当前主后端建议保持关闭」,
   避免运维误以为开了就能省冷启动。

5. **修订 ADR-0008 §3 与 ADR-0012 的指向。** 在 §3 warm-pool 段补一句:在 OpenSandbox-only
   语境下默认走按需分配,warm pool 可选且当前无后端可 adopt,详见本 ADR。

## Alternatives Considered

- **A. 维持 ADR-0012 现状(warm pool 摆在主路径、DaemonSet 镜像预拉)。** 否决:DaemonSet
  预拉是 K8s provider 专属,而 k8s provider 已被 ADR-0014 删除;在 OpenSandbox-only 部署里
  这条路径没有载体。

- **B. 为 OpenSandbox 实现 `provider.Adopter`(领用后热挂卷)。** 否决:实测 OpenSandbox
  运行中沙箱的 API 不提供增删卷端点,volumes 仅创建时可指定。技术上不可实现。

- **C. 预热箱挂 userdata 根、领用时按 uid 进子目录。** 否决:把全体用户数据根挂进无身份
  预热箱 = 跨用户越权,违反 ADR-0008 per-user 卷隔离。(同 ADR-0012 备选 B。)

- **D. 删掉 warm pool 全部代码。** 否决:它是干净的可选能力隔离(ADR-0002),保留缝口
  成本极低,且为未来支持卷热挂的 backend 留口;删了将来要重写。改为「默认关闭 + 文档
  说明」即可。

- **E. 默认按需分配 + warm pool 可选(选定方案)。** 零代码回退、零架构妥协、与 OpenSandbox
  API 契约一致,匹配自托管中小并发的实际负载。

## Consequences

- **Positive**
  - 默认路径干净、可解释:来一个会话 create 一个箱、创建时挂双卷,已真 server 验收。
  - 绕开 OpenSandbox 不支持运行中热挂卷这堵墙,无任何架构妥协。
  - warm pool 引擎、Adopter 缝口、ADR-0012 全部无需改动,本 ADR 是策略澄清而非代码回退。

- **Negative / 接受的代价**
  - 每个新会话付完整冷启动(镜像由 OpenSandbox server 侧缓存 + 沙箱初始化 + 进程启动)。
    对中小并发可接受;高并发、对首字延迟敏感的部署需另做预测性预创建(留待数据驱动)。
  - warm pool 成为「保留但默认不启用、当前主后端无收益」的能力,需在文档与启动日志中
    讲清,以免误用。

- **Followups**
  - main.go:增强 warm-pool-enabled-but-no-Adopter 的启动告警文案。
  - 端到端 chat 验收:gateway → agent-runtime → sandbox-manager → OpenSandbox,确认按需
    分配主路径对话闭环(本 ADR 关联任务 #28)。
  - 未来若需 OpenSandbox 预热收益:起 ADR 评估「按 (user,session) 预测性预创建」。
  - README 路线图:把 warm pool 标注为「可选、默认关闭」。

## Amendment (2026-06-30, ADR-0016)

本 ADR 的策略表态(**按需冷启动分配为默认且唯一主路径**)继续有效。但其备选 D
「保留 warm pool 全部代码」已由 **ADR-0016 推翻**:warm pool 能力(warmpool 包、
`provider.Adopter` 缝口、binder 的 tryAdopt、metrics 的 pooled 维度、main.go 接线与
`COCOLA_WARMPOOL_*` env)已整体移除。理由:OpenSandbox 上的不兼容是永久性的、保留缝口
只带来维护负担、未来真要预热收益需另起机制(预测性预创建)。详见 ADR-0016。
