# changelog: ADR-0013 起草 — OpenSandbox 作为可插拔 SandboxProvider 后端

## 背景

用户提出把高 star 开源 agent 沙箱项目 OpenSandbox(阿里开源,Apache-2.0,
~11.6k stars,FastAPI 控制面 + Docker/K8s production-ready 运行时 + gVisor 隔离 +
SandboxSnapshot pause/resume)作为 cocola 的沙箱系统,要求评估「是否可重构
opensandbox 集成进 cocola」。

评估结论:**能用,但不该整体替换**。OpenSandbox 与 cocola 沙箱层(#12 Vault、
egress NetworkPolicy、#15 gVisor、warm pool)高度重叠,整体替换会推翻已投入工作并
引入 Go↔Python 进程边界;正确姿势是按 ADR-0002「新后端=新包+Register」封装为可插拔
后端。本次先落 ADR + PoC 任务清单,不动任何代码。

## 改动

- 新增 `docs/adr/0013-opensandbox-as-pluggable-provider.md`(Status: Proposed)。
  - Decision:把 OpenSandbox 封装成新 `provider/opensandbox` 后端,
    `Register("opensandbox", …)`,8 方法映射 REST `/v1/sandboxes` 生命周期;
    不替换现有 docker/k8s 沙箱层。
  - Alternatives:A 整体替换(否决)、B 封装为后端(选定)、C 仅借鉴设计(退路)。
  - Consequences 记录三个关键未知:Go↔Python 进程边界、Exec 流式语义映射风险
    (cocola Exec 返回 `<-chan ExecEvent`)、Vault/egress 能力归属、快照可移植性。
- 更新 `docs/adr/README.md` 索引,补 0013 行。

## 配套任务(TaskList)

- #18 主任务:PoC OpenSandbox 封装为可插拔 SandboxProvider(被 #19/#20/#21 阻塞)。
- #19 P0:本机用 uv 起 OpenSandbox server,摸清 REST/SDK/流式 exec/部署依赖。
- #20 P1:provider/opensandbox 最小骨架(Create/Health/Destroy)+ httptest 单测,
  仅新增包,不动核心接口与 docker/k8s 后端。
- #21 P2:验证 Exec 流式 + Pause/Resume(SandboxSnapshot)映射,回填 ADR-0013。
- 依赖链:#19 → #20 → #21 → 解阻 #18。

## 边界

- 本提交仅 ADR + changelog,零代码改动。任何 provider 代码改动待 #18 PoC 并经用户
  确认后启动。
- 与 #17(Agent Substrate 评估)并列:二者同为「外部运行时按 ADR-0002 封装为可插拔
  后端」的候选,ADR-0013 是该模式首个具体实例。
