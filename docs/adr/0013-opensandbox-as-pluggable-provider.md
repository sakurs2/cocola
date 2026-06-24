# ADR-0013: 将 OpenSandbox 作为可插拔 SandboxProvider 后端(而非替换沙箱层)

- Status: Proposed
- Date: 2026-06-24
- Deciders: @cocola-maintainers
- Depends on: ADR-0002（SandboxProvider 抽象铁律）、ADR-0008（持久化分层与 K8s/gVisor 后端）、ADR-0012（warm pool 预热策略)

## Context

cocola 的沙箱层目前由 `apps/sandbox-manager` 的 `SandboxProvider` 抽象统领,核心
契约是 8 个方法(Create / Exec / WriteFile / ReadFile / Pause / Resume / Destroy /
Health),外加可选能力接口 `provider.Adopter`;后端通过 `init()` 里的
`provider.Register(name, impl)` 自注册(ADR-0002)。现已落地 docker、k8s 两个后端,
并在 #12(Vault)、egress NetworkPolicy、#15(gVisor + 镜像预拉)上自建了隔离 /
出口 / 凭据 / 冷启动一系列能力。

社区出现了一个高成熟度的同问题域开源项目 **OpenSandbox**(阿里开源,Apache-2.0,
约 11.6k stars,自述「Secure, Fast, and Extensible Sandbox runtime for AI
agents」)[[OpenSandbox]](https://github.com/opensandbox-group/OpenSandbox)。其能力与
cocola 沙箱层高度重叠:Docker 与 Kubernetes 两种运行时均标注 production-ready、
FastAPI 控制面暴露 REST `/v1/sandboxes` 生命周期(create/start/pause/resume/delete)、
`OPEN-SANDBOX-API-KEY` 头鉴权、K8s 的 pause/resume 走 BatchSandbox CRD +
SandboxSnapshot[[OpenSandbox server]](https://github.com/opensandbox-group/OpenSandbox/blob/main/docs/components/server.md);
此外还提供 gVisor / Kata / Firecracker 强隔离、ingress 网关 + per-sandbox egress 管控、
Credential Vault(注入机密而不暴露真实 secret),以及多语言 SDK(含 Go)+ osb CLI +
MCP server[[OpenSandbox]](https://github.com/opensandbox-group/OpenSandbox)。

问题:cocola 奉行「尽量复用开源、避免造轮子」。OpenSandbox 远比之前评估的 Agent
Substrate(自述 v0.0.0、不可生产)成熟,是一个真实的复用候选。但它与 cocola 的
重叠是**双刃剑**——这几乎全部能力 cocola 已自建或正在自建,且 OpenSandbox 控制面是
Python FastAPI,与 cocola 的 Go `sandbox-manager` 之间隔着一条进程 / 语言边界。

需要回答的是:**该不该、以何种方式把 OpenSandbox 并入 cocola 的沙箱系统?**

本 ADR 只做架构决策与集成路径选型,**不含任何代码改动**;真正落地由后续 PoC task
驱动,PoC 数据回填后再决定是否扩大。

## Decision

**采用「选择性复用、适配接入」:把 OpenSandbox 封装成 cocola 的一个新
`SandboxProvider` 后端(新建 `provider/opensandbox` 包,在 `init()` 里
`Register("opensandbox", …)`),内部通过 OpenSandbox 的 Go SDK / REST 调用其
FastAPI server,把 8 个核心方法映射到 `/v1/sandboxes` 生命周期。不整体替换 cocola
现有的 docker / k8s 沙箱层。**

理由:

1. **遵守 ADR-0002 铁律。** 「新后端 = 新包 + Register」正是 ADR-0002 预留的扩展点;
   核心 `SandboxProvider` 接口与现有 docker / k8s 后端**零改动**,新后端与它们平等
   共存,可按部署配置切换、可做 A/B 对照。这是引入任何外部运行时的唯一合规姿势。

2. **生命周期天然对齐。** OpenSandbox 的 REST 生命周期
   (create/start/pause/resume/delete)与 cocola 的
   Create/Pause/Resume/Destroy 近乎一一对应,映射成本低;其 K8s pause/resume
   (SandboxSnapshot)甚至可能直接满足 #15 一直悬而未决的 RAM-kept resume 诉求
   [[OpenSandbox server]](https://github.com/opensandbox-group/OpenSandbox/blob/main/docs/components/server.md)。

3. **复用收益最大、回退风险最小。** 封装为后端能直接复用 OpenSandbox 已生产化的
   隔离 / 快照 / 运行时实现,而不必推翻 cocola 已投入的工作;一旦 PoC 不达预期,
   删掉该后端包即可,不波及主路径。

4. **能力归属冲突显式留待 PoC 决策,不在本 ADR 拍死。** cocola 已有 #12 Vault、
   egress NetworkPolicy 与 OpenSandbox 的同名能力重叠;采用该后端时,是停用 cocola
   自有管控、还是与之并存,由 PoC 实测后另行决定,避免双重管控。

## Alternatives Considered

- **A. 整体替换 cocola 沙箱层。** 拆掉 docker / k8s 后端,全栈改用 OpenSandbox
  server 作为唯一运行时。否决:推翻 #12 / egress / #15 已投入的工作;把一条 Python
  FastAPI 控制面与配套 CRD 变成 cocola 的强依赖与运维负担;违背 ADR-0002「核心接口
  稳定 + 增量扩展」与项目的增量原则。一次性迁移的回归面过大。

- **B. 封装为可插拔 `SandboxProvider` 后端(选定方案)。** 见上。零接口改动、可共存、
  可回退,是「复用开源」与「ADR-0002 抽象」的交集。

- **C. 仅借鉴设计,不接 server。** 只参考其 SandboxSnapshot / CRD / egress 设计来
  补强 cocola 自有实现,不引入 OpenSandbox 本体。部分否决:这是 #15 探针 6 对 Agent
  Substrate 已采取的姿势,适合「不愿引入进程依赖」时的兜底;但对一个生产可用、
  Apache-2.0 的项目,放弃复用其成熟代码、只抄思路再自己写一遍,恰恰是「造轮子」。
  保留为 B 不成立时的退路,非首选。

## Consequences

- **Positive**
  - 直接复用 OpenSandbox 已生产化的 Docker/K8s 运行时、gVisor 隔离、SandboxSnapshot
    快照,避免在 cocola 内重复造同类能力。
  - `SandboxProvider` 核心接口、docker/k8s 后端、warm-pool 引擎与降级逻辑全部无需
    改动——本 ADR 是新增可选后端,不是回退或重写。
  - 为 #15 悬而未决的 RAM-kept resume 提供了一条可验证的现成实现路径。

- **Negative / 接受的代价**
  - 引入一条 **Go ↔ Python(FastAPI)进程边界**:启用该后端时部署拓扑要多一个
    OpenSandbox server 组件,需评估运维成本与可接受性。
  - **Exec 流式语义存在映射风险**:cocola 的 `Exec` 返回 `<-chan ExecEvent`(流式),
    需确认 OpenSandbox REST 是否提供等价的流式 exec/attach,否则映射有损——列为 PoC
    的关键验证项。
  - **能力重叠需治理**:Vault / egress 与 cocola 已有实现的取舍未在本 ADR 定死,
    存在「双重管控」隐患,留待 PoC。
  - **快照可移植性未知**:其 K8s pause/resume 依赖自定义 CRD,需确认与 cocola 现有
    namespace / NetworkPolicy 模型兼容。

- **Followups**
  - 新建 PoC task:实现 `provider/opensandbox` 最小骨架(Create / Health / Destroy
    三方法打通 REST),验证进程边界、生命周期映射、Exec 流式语义三个关键未知;
    PoC 前不动任何现有 provider 代码。
  - PoC 数据回填后,再决定是否扩大到 8 方法全实现,以及 Vault / egress 能力归属。
  - 与 #17(Agent Substrate 评估)并列跟踪;二者同为「外部运行时按 ADR-0002 封装为
    可插拔后端」的候选,本 ADR 为该模式的首个具体实例。
