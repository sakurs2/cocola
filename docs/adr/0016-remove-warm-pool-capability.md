# ADR-0016: 移除 warm pool 能力

- Status: Accepted
- Date: 2026-06-30
- Deciders: @cocola-maintainers
- Supersedes: ADR-0012(warm pool 在 PVC/bind-mount 卷模型下的预热策略修订)
- Amends: ADR-0015(推翻其备选 D「保留 warm pool 缝口」;默认按需分配的策略表态仍有效)
- Depends on: ADR-0002(SandboxProvider 可插拔铁律,不变)、ADR-0014(OpenSandbox 唯一主后端)、ADR-0008(双卷持久化模型)

## Context

warm pool 的设计初衷(ADR-0008 §3、任务 #13):后台预热一批空闲沙箱,新会话来时
"领用"一个并后挂该用户的卷(adopt-by-remount),把"镜像拉取 + 容器启动"的冷启动
延迟从请求路径里挪走。落地时实现为可选能力 `provider.Adopter` + backend 无关的
`warmpool.Pool` 引擎 + binder 的 `tryAdopt` 接入,`SandboxProvider` 核心接口零改动。

此后该能力被两次收敛逼到鸡肋位置:

1. **ADR-0012**:Docker / K8s 两后端都无法给运行中的容器/Pod 热挂卷,adopt-by-remount
   主路径不成立;K8s 改走 DaemonSet 节点镜像预拉。
2. **ADR-0014**:后端收敛为 OpenSandbox 唯一主力 + docker 兜底,**k8s provider 已删除**
   —— DaemonSet 预拉这条 K8s 专属路径随之失去载体。
3. **ADR-0015**:实测正在运行的 OpenSandbox server 的 OpenAPI,对已存在沙箱仅暴露
   `PATCH metadata` / `pause` / `resume` / `renew-expiration` / `snapshots` / `proxy/*`,
   **没有任何给运行中沙箱增删卷的接口**;`volumes` 只能在 `POST /sandboxes` 创建时
   一次性指定。adopt-by-remount 在唯一主后端上**永久不可实现**。遂把"按需冷启动分配"
   定为默认且唯一主路径,warm pool 降级为"默认关闭的可选优化"。

ADR-0015 当时在备选 D 里**否决了"删掉 warm pool 全部代码"**,理由是"保留缝口成本低、
为未来支持卷热挂的 backend 留口"。本 ADR 重新评估并推翻这一保留决定,依据:

- **不兼容是永久性的,不是临时限制。** OpenSandbox 的卷契约(仅创建时挂卷)决定了
  adopt-by-remount 这条路根本走不通。真要给 OpenSandbox 预热收益,正确方向是
  **"按 (user, session) 预测性预创建带身份的箱"**(预热时就把用户卷挂上,而非建无身份
  箱再领用)——那是另一套机制,与现有 warm pool 引擎(建无身份箱 → 领用 → 后挂卷)
  **不复用、得重写**。所谓"留口"留不住未来真正要用的东西。
- **目标负载是自托管中小并发。** binder 的按需路径已在真 OpenSandbox server 上验收
  通过,冷启动那几秒对这类部署完全可接受(ADR-0015 已确认)。
- **保留的不是"一个缝口",是持续的认知与维护负担。** warmpool 包、binder 的 `tryAdopt`
  分支、`provider.Adopter` 接口、metrics 的 `pooled` 维度、main.go 一段"开了也只空转"
  的告警接线、4 个测试文件、6 个 `COCOLA_WARMPOOL_*` env 变量,都要跟着每次重构走,
  且读代码时都得解释"这玩意儿默认关、当前后端还用不了"。

## Decision

**彻底移除 warm pool 能力,代码与文档全部清除;sandbox-manager 仅保留按需冷启动
分配主路径。** 具体:

1. **删代码。** 删除 `internal/orchestrator/warmpool` 整个包(含 redis `warm:` 子命名空间
   读写);删 binder 的 `pool` 字段 / `WithWarmPool` / `tryAdopt` / `recordPooled` 与
   `AcquireWithOutcome` 中的 warm fast-adopt 分支;删 `provider.Adopter` 接口;删 metrics
   的 `pooled` 维度;删 main.go 的 warm 构造 / 接线 / 告警;移除全部 `COCOLA_WARMPOOL_*`
   env 读取。`SandboxProvider` 核心 8 方法接口不动。

2. **binder 退回两条路。** 快路径(已有绑定 → 续租复用)/ 慢路径(加锁 → 双检 →
   `provider.Create` 并在创建时映射双卷)。这正是 ADR-0015 验收通过、与 OpenSandbox
   API 契约一致的路径,本次零改动。

3. **保留 hit/miss 复用指标。** `cocola_sandbox_pool_*` 指标度量的是 binder 的
   session→sandbox **复用率**(hit/miss),与 warm pool 无关,**指标名保留不改**
   (改名会破坏既有 Grafana 看板/告警);仅清理 help 文案中"warm sandbox"等会误导的措辞,
   并删掉从不被采集的 `PooledCount`。

4. **不引入替代预热机制。** 明确不做"预测性预创建"等任何新预热路径。若未来确有高并发、
   首字延迟敏感的需求,需由冷启动实测数据驱动、另起 Plan/ADR 评估,与本次移除互不阻塞。

5. **文档收口。** ADR-0012 标 `Superseded by ADR-0016`;ADR-0015 补注"保留可选"部分
   由本 ADR 收敛(默认按需分配仍有效);ADR-0008 §3 / 0009 / 0013 / 0014 提及处补指向;
   docs/adr/README.md 更新;docs/plan/hardening-warm-pool.md 加废弃横幅;README 路线图
   WP 行改"已移除"。

## Alternatives Considered

- **A. 维持 ADR-0015 现状(保留代码、默认关闭)。** 否决:不兼容是永久性的,保留只带来
  持续的认知/维护负担,且"留口"留不住未来真正需要的"预测性预创建"机制——后者要重写。

- **B. 现在就为 OpenSandbox 实现预测性预创建(带身份预热)。** 否决:超出当前范围,
  应由冷启动实测数据驱动;且它与现有 warm pool 引擎不复用,删除现有能力不会增加将来成本。

- **C. 删除全部 warm pool 代码与文档(选定方案)。** 默认主路径不变、已验收、与
  OpenSandbox API 契约一致;消除"默认关却空转"的迷惑态与长期维护负担;纯删除型改动,
  可一次 `git revert` 回滚,代码仍在 git 历史。

## Consequences

- **Positive**
  - sandbox-manager 只剩一条清晰、已验收的分配路径(create-on-demand + 创建时挂双卷),
    代码与文档不再需要解释一个"默认关、当前后端用不了"的能力。
  - 移除约一个包 + 4 个测试文件 + 6 个 env 变量 + binder/metrics/main.go 多处接线,
    降低维护面与重构成本。
  - 复用率指标(hit/miss/active/create_p99)与既有看板/告警完全不受影响。

- **Negative / 接受的代价**
  - 每个新会话付完整冷启动(镜像由 OpenSandbox server 侧缓存 + 沙箱初始化 + 进程启动)。
    对自托管中小并发可接受;高并发、首字延迟敏感的部署若有需求,需另做预测性预创建。
  - 失去一个"可选优化缝口";但该缝口在唯一主后端上永久无收益,且未来要的是另一套机制,
    保留无实益。代码仍在 git 历史中,需要时可考古。

- **Followups**
  - 移除后跑 `GOWORK=off go build/vet/test ./...` 全绿、gofmt 干净,写 changelog。
  - 后续若评估预测性预创建,另起 ADR,由容量基线/冷启动实测数据驱动。
