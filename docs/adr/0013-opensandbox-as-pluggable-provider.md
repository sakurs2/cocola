# ADR-0013: 将 OpenSandbox 作为可插拔 SandboxProvider 后端(而非替换沙箱层)

- Status: Accepted（PoC P0–P2 离线验证 + 真 server 端到端实测于 2026-06-28 全链路通过）
- Date: 2026-06-24（PoC 回填 2026-06-26;真 server 实测回填 2026-06-28）
- Deciders: @cocola-maintainers
- Depends on: ADR-0002（SandboxProvider 抽象铁律）、ADR-0008（持久化分层与 K8s/gVisor 后端）、ADR-0012（warm pool 预热策略,已由 ADR-0016 移除)

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

## PoC Findings（#18 / P0–P2,2026-06-26 回填）

落地由 #19(P0)/#20(P1)/#21(P2)三段 PoC 驱动。受「沙箱内禁止起监听进程」约束,
PoC 全程未启动任何 OpenSandbox server/execd,改以**源码 + OpenAPI 规约静态走查**
(下载 tarball)+ **RoundTripper / SSE stub 单测**完成验证,真 server 端到端实测
(resume 延迟、能力归属)留待合规环境。三个关键未知全部得到正向结论:

1. **Exec 流式语义——已解除风险(原列为最大未知)。** OpenSandbox 的 exec 不在
   lifecycle server 上,而在每个沙箱内的独立 **execd** 服务;调用是两步:先经
   lifecycle `GET /v1/sandboxes/{id}/endpoints/44772` 解析出 execd 可达地址 + 鉴权头
   (`X-EXECD-ACCESS-TOKEN`),再对 `{execd}/command` 发起 `Accept: text/event-stream`
   的 SSE/NDJSON 流。其事件类型(`stdout`/`stderr`/`error`/`execution_complete`/
   `init`/`ping`)可**无损**桥接到 cocola 的 `<-chan ExecEvent`:stdout/stderr 直映,
   `error` 的 evalue 能 `Atoi` 时映射为退出码(否则为错误事件),`execution_complete`
   在无错时补 exit 0。结论:cocola 的流式 Exec 契约**完全可满足**,无降级。

2. **生命周期映射——一一对应已确认。** Create→`POST /v1/sandboxes`、
   Health→`GET /v1/sandboxes/{id}`、Destroy→`DELETE …`、Pause→`POST …/pause`、
   Resume→`POST …/resume`,与 cocola 8 方法近乎同构。

3. **能力重叠属实——`CreateSandboxRequest` 内建 `NetworkPolicy`(↔ cocola egress)、
   `CredentialProxy`(↔ #12 Vault)、`Volumes`(↔ K8s 卷模型)、`SnapshotID`。**
   归属取舍仍留待真环境实测,PoC 已确认字段层面可承接 cocola 的 egress allowlist
   映射(default-deny + per-domain allow)。

PoC 产出(均不改动现有 provider):`provider/opensandbox` 包实现 Create/Health/
Destroy/Exec(流式)/Pause/Resume 六方法 + `newProvider` 工厂接线;WriteFile/ReadFile
(映射 execd 上传/下载)留为 `errNotImplemented`,不在流式 exec 关键路径上。REST
客户端与 SSE 桥接均为 **stdlib-only(零外部依赖)**,17 个单测(含 SSE 流桥接、
退出码解析、Pause/Resume 端点断言)全绿,`-race` 干净。

详见 `docs/archive/opensandbox-poc-p0-research.md`、`…-p1-provider-skeleton.md`、
`…-p2-exec-pause-resume.md`。

## Consequences

- **Positive**
  - 直接复用 OpenSandbox 已生产化的 Docker/K8s 运行时、gVisor 隔离、SandboxSnapshot
    快照,避免在 cocola 内重复造同类能力。
  - `SandboxProvider` 核心接口、docker/k8s 后端、warm-pool 引擎与降级逻辑全部无需
    改动——本 ADR 是新增可选后端,不是回退或重写。(注:warm-pool 引擎此后已由
    ADR-0016 整体移除,与本 ADR 的"可插拔后端"结论无关。)
  - 为 #15 悬而未决的 RAM-kept resume 提供了一条可验证的现成实现路径。

- **Negative / 接受的代价**
  - 引入一条 **Go ↔ Python(FastAPI)进程边界**:启用该后端时部署拓扑要多一个
    OpenSandbox server 组件,需评估运维成本与可接受性。
  - ~~**Exec 流式语义存在映射风险**~~ → **PoC 已解除**:execd 的 SSE/NDJSON
    `/command` 流可无损桥接到 cocola 的 `<-chan ExecEvent`(见 PoC Findings 第 1 条);
    代价是 Exec 需多一跳 `GET …/endpoints/44772` 解析 execd 地址。
  - **能力重叠需治理**:Vault / egress 与 cocola 已有实现的取舍未在本 ADR 定死,
    存在「双重管控」隐患,留待 PoC。
  - **快照可移植性未知**:其 K8s pause/resume 依赖自定义 CRD,需确认与 cocola 现有
    namespace / NetworkPolicy 模型兼容。

- **Followups**
  - ~~新建 PoC task:实现 `provider/opensandbox` 最小骨架~~ → **已完成**
    (#18/#19/#20/#21):Create/Health/Destroy/Exec/Pause/Resume 六方法落地 + 单测;
    进程边界、生命周期映射、Exec 流式语义三个关键未知均已离线验证通过。
  - ~~待真 server 端到端实测~~ → **已完成(2026-06-28,本机 OrbStack/Docker)**:
    启用 OpenSandbox server(一站式 `make verify-opensandbox-full`,
    `deploy/docker-compose/docker-compose.opensandbox.yml` + `cmd/opensandbox-verify`
    harness),Create→Health→Exec(流式,含退出码 / ~1MiB stdout / 多行)→文件往返→
    Pause→Resume→Destroy **全链路一次通过(`VERIFY OK — all stages passed`)**。
    实测 resume:Resume 调用 ~10ms 受理、~13ms 内回到 Running,Pause 前写入的标记文件在
    Resume 后仍在——为 #15 的 RAM-kept resume 提供了实测量级佐证。实测中暴露并修复了 4 个
    「仅真 server 才触发」的缺陷:(1)create 必须带非空 `entrypoint`(provider 注入
    `["tail","-f","/dev/null"]`,长生命周期沙箱模型);(2)有 image 时必须带
    `resourceLimits`(harness `-cpu/-mem` 默认值 0.5/512);(3)bridge 网络下 execd 端点
    需走 server proxy(`?use_server_proxy=true`,默认开,可经 `COCOLA_OPENSANDBOX_DIRECT_EXEC`
    退回直连);(4)execd `/command` 收单条 shell 字符串会二次 shell 解析,argv 必须
    逐元素单引号转义(`shellJoin`)而非朴素空格拼接。详见
    `docs/archive/fix-opensandbox-real-server-e2e.md`。Vault / egress 能力归属与
    WriteFile/ReadFile 补齐维持原决策(暂留 cocola 侧 / `errNotImplemented`),不阻塞本 ADR。
  - 与 #17(Agent Substrate 评估)并列跟踪;二者同为「外部运行时按 ADR-0002 封装为
    可插拔后端」的候选,本 ADR 为该模式的首个具体实例。
  - **后续(ADR-0014)**:基于本 ADR 的真 server e2e 结论,已决定将 OpenSandbox 定为
    **主力沙箱后端**、退役自建 k8s provider(docker 保留为零配置兜底),#17 一并关闭。
    可插拔架构(ADR-0002)保留不变。
