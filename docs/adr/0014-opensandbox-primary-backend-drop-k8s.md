# ADR-0014: OpenSandbox 定为主力沙箱后端,退役 k8s provider(docker 保留为兜底)

- Status: Accepted
- Date: 2026-06-28
- Deciders: @cocola-maintainers
- Supersedes: ADR-0013 中"仅作可插拔后端、不表态主次"的措辞;收敛 ADR-0008 / ADR-0002 对 K8s+gVisor 这一**具体后端**的预期
- Depends on: ADR-0002（SandboxProvider 可插拔铁律,保留不变）、ADR-0013（OpenSandbox provider 已通过真 server e2e）

## Context

cocola 沙箱层由 ADR-0002 的 `SandboxProvider` 8 方法抽象统领,后端可插拔。截至本 ADR
已落地三个后端:docker(567 LOC,零依赖、零配置)、k8s（~1900 行含测试,重依赖
client-go)、opensandbox（694 LOC,纯 stdlib REST/SSE 客户端)。OpenSandbox provider
已在本机真 server 上跑通 Create→Health→Exec(流式)→文件往返→Pause→Resume→Destroy 全链路
(ADR-0013、commit b224d3b)。

OpenSandbox 是高成熟度同问题域开源项目(Apache-2.0,Docker 与 K8s 两种运行时均
production-ready、内建 gVisor 隔离与 SandboxSnapshot 快照)[[OpenSandbox]](https://github.com/opensandbox-group/OpenSandbox)。
其能力与 cocola 自建的 k8s+gVisor 后端高度重叠。继续同时维护自建 k8s provider 与
OpenSandbox,等于在同一问题域重复造轮子,违背「优先复用开源、避免造轮子」的工程原则。

决策诉求:**集中投入 OpenSandbox 作为主力后端;停止自建沙箱运行时(尤其 k8s+gVisor)
的继续投入。** 同时保留可插拔架构本身,不回退为单后端硬编码。

What we explicitly do **not** change: `SandboxProvider` 接口、工厂选择、provider 自注册
机制(ADR-0002 铁律);provider 之上的编排能力(warm pool〔后由 ADR-0016 移除〕/ egress / Vault),它们与
具体后端解耦。

## Decision

1. **保留 ADR-0002 可插拔架构**:沙箱仍是「接口 + 多后端 + 工厂选择」,不硬编码单后端。
2. **OpenSandbox 为主力后端**:生产推荐、文档默认、后续沙箱能力的主要投入方向。
3. **退役 k8s provider**:删除 `internal/provider/k8s` 全部 Go 代码与其工厂接线,
   移除 client-go 等依赖。自建 K8s+gVisor 运行时不再演进——该问题域改由 OpenSandbox
   的 K8s 运行时承接。
4. **docker provider 保留为兜底**:零依赖、零配置,服务于本地单进程调试与
   降级。因此**运行时默认 provider 维持 `docker`**(`COCOLA_SANDBOX_PROVIDER` 默认值
   不变)——`opensandbox.New()` 强依赖 `COCOLA_OPENSANDBOX_URL`,若钉为进程默认会让
   无 env 的本地 `go run` / CI 直接启动失败。「主力」体现在定位、文档与生产部署默认,
   而非进程级默认值。
5. **k8s 相关部署物 / 历史文档**(`deploy/k8s`、`deploy/helm/cocola-sandbox`、
   `docs/plan/m6-*`、gVisor spike plan、m6 runbook)标注 superseded 但**不删除**:
   属可逆资产与历史记录,删 YAML 超出本次范围。

## Alternatives Considered

- **全删 docker、只留 opensandbox 单后端** — 最彻底,但本地起 cocola 即强依赖一个
  外部 OpenSandbox server,调试链路变重,且违背 ADR-0002 可插拔承诺。拒绝。
- **三后端全保留、仅改文档定位** — 改动最小最可逆,但 k8s provider 持续占用维护成本
  (client-go 升级、gVisor 验收、egress 收口),与「停止其余沙箱投入」的诉求矛盾。拒绝。
- **把默认 provider 直接切 opensandbox** — 符合「主力」直觉,但破坏零配置本地启动。
  以「docker 兜底默认 + 文档/生产推荐 opensandbox」替代。拒绝。

## Consequences

- **Positive**
  - 单一主力后端,维护面收敛;移除 client-go 及大批间接依赖,sandbox-manager 构建更轻。
  - 复用 OpenSandbox 生产化的 Docker/K8s 运行时、gVisor、快照,不再自建同类能力。
  - 可插拔架构与 docker 兜底保留,本地调试零配置体验不变。
- **Negative / 接受的代价**
  - 失去自建 k8s+gVisor 后端;生产强隔离与 K8s 调度能力今后绑定 OpenSandbox 的实现与
    运维(需部署 OpenSandbox server)。
  - 自建 k8s provider 上沉淀的 egress NetworkPolicy / Vault Agent / 镜像预拉等 K8s 专属
    能力随之退场,如未来需要须经 OpenSandbox 重新表达。
- **Followups**
  - 关闭 #15(gVisor spike)、#17(Agent Substrate 评估):收敛到单一主力后端,二者不再推进。
  - deploy/k8s、helm、m6 plan/runbook、gVisor plan 加 superseded 横幅(本 ADR 同批执行)。
  - 后续若生产上线 OpenSandbox,补一份 OpenSandbox server 部署 runbook(独立任务)。
