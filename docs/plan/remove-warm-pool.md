# Plan: 移除 warm pool 能力(代码 + 文档)

> 状态:规划中(2026-06-30)。本文先评审、后动代码(遵循"非平凡改动先写 Plan")。
> 关联:ADR-0008 §3「Warm pool」、ADR-0012(warm pool 预热策略)、ADR-0015
> (默认按需分配、warm pool 可选)、ADR-0002(Provider 抽象铁律);任务 #13/#16。
> 决策载体:本 Plan 落地需配套 **ADR-0016「移除 warm pool 能力」**,supersede
> ADR-0012,并显式推翻 ADR-0015 备选 D(当时"保留缝口")。

## 1. 背景与动机

warm pool 的初衷(ADR-0008 §3 / 任务 #13):后台预热一批空闲沙箱,新会话来时
"领用"一个并后挂该用户的卷(adopt-by-remount),把镜像拉取 + 容器启动的冷启动
延迟从请求路径里挪走。

随后两次收敛已把它逼到"鸡肋"位置:

1. **ADR-0012**:Docker / K8s 两后端都无法给运行中的容器/Pod 热挂卷,
   adopt-by-remount 主路径不成立;K8s 改走 DaemonSet 节点镜像预拉。
2. **ADR-0014**:后端收敛为 OpenSandbox 唯一主力 + docker 兜底,k8s provider 删除
   —— DaemonSet 预拉这条 K8s 专属路径随之失去载体。
3. **ADR-0015**:实测 OpenSandbox 运行中沙箱 **没有任何增删卷的 API**(volumes 仅
   `POST /sandboxes` 创建时可指定),adopt-by-remount 在唯一主后端上**永久不可实现**;
   遂把按需冷启动定为默认且唯一主路径,warm pool 降级为"默认关闭的可选优化"。
   但 ADR-0015 备选 D(删掉全部代码)被否决,理由是"保留缝口成本低、为未来卷热挂
   backend 留口"。

现在重新评估这个"保留"决定:

- **不兼容是永久性的**,不是临时限制。OpenSandbox 的卷契约决定了 adopt-by-remount
  这条路根本走不通;真要给 OpenSandbox 预热收益,正确方向是"按 (user,session)
  预测性预创建带身份的箱"——那是另一套机制,与现有 warm pool 引擎(建无身份箱再领用)
  **不复用、得重写**。所谓"留口"留不住未来真正要的东西。
- **目标负载是自托管中小并发**,冷启动那几秒可接受(ADR-0015 已确认)。
- **保留的成本不只是"一个缝口"**:warmpool 包 + binder 的 tryAdopt 分支 +
  provider.Adopter 接口 + metrics 的 pooled 计数 + main.go 一段"开了也空转"的告警
  接线 + 4 个测试文件 + 5 处 env 变量,都是长期要跟着重构走、读代码时要解释"这玩意儿
  默认关、当前后端还用不了"的认知负担。
- 用户明确决策:**移除该能力,以后都不再做**。

结论:把 warm pool 从"保留但默认关"进一步收敛为"彻底移除",代码与文档全部清除。

## 2. 范围与非目标

**做**:删除 warm pool 全部代码与文档引用;binder 退回"快路径复用 / 慢路径
cold create"两条路;metrics 去掉 pooled 维度;写 ADR-0016 记录决策;旧 ADR 标
superseded / 补注。

**不做**:
- 不动 binder 的按需分配主路径(快路径续租复用、慢路径加锁双检 + 创建时挂双卷)——
  那是 ADR-0015 已验收的主路径,本次零改动。
- 不动 provider 的核心 8 方法接口契约(只删可选的 Adopter 缝口)。
- 不引入"预测性预创建"等任何替代预热机制(明确不再做)。
- 不动 egress / Vault / 双卷映射等其它编排能力。

## 3. 删除清单(代码)

### 3.1 整体删除
- `apps/sandbox-manager/internal/orchestrator/warmpool/pool.go`
- `apps/sandbox-manager/internal/orchestrator/warmpool/pool_test.go`
  (整个 warmpool 包,含 redis `warm:` 子命名空间的全部读写)
- `apps/sandbox-manager/internal/orchestrator/warmpool_binder_test.go`(warm 接入测试)
- `binder_opensandbox_live_test.go` 中 warm 相关注释/路径(文件本身保留,清注释)

### 3.2 binder.go(internal/orchestrator)
- 删 `import .../warmpool`
- 删 `Binder.pool` 字段、`WithWarmPool` 方法
- 删 `AcquireWithOutcome` 里的 warm-pool fast adopt 分支(`tryAdopt` 调用 +
  `recordPooled`),慢路径直接 `provider.Create`
- 删 `tryAdopt` 方法
- 删 `recordPooled` 方法

### 3.3 provider.go(internal/provider)
- 删 `Adopter` 接口及其文档块(核心 `SandboxProvider` 8 方法不动)

### 3.4 metrics.go(internal/orchestrator)
- 删 `pooled atomic.Int64` 字段、`recordPooled`、`Snapshot.PooledCount`
- 顶部注释里 pool 语义微调(hit/miss 仍保留,仍是复用率信号)

### 3.5 obs/collector.go + collector_test.go
- 指标名 `cocola_sandbox_pool_*` 保留(它们度量的是 binder 复用 hit/miss,不是
  warm pool,改名会破坏既有 Grafana/告警),仅把 help 文案里"warm sandbox"措辞
  改为中性的"reused sandbox / cold-created",避免误导。PooledCount 既未被 collector
  采集,删字段不影响该包。

### 3.6 main.go(cmd/sandbox-manager)
- 删 `import .../warmpool`
- 删 warm pool 构造 + `binder.WithWarmPool(pool)` + `COCOLA_SANDBOX_IMAGE` 作为
  poolImg 的用法 + `pool.Enabled()` 整段(含 no-Adopter 告警、`pool.Run`、日志)
- binder 接线退回 `NewBinder(...).WithMetrics(bm).WithNetworking(net)`

### 3.7 env 变量
- 移除 `COCOLA_WARMPOOL_ENABLED / MIN_IDLE / MAX / REFILL_SECS /
  MAX_LIFETIME_SECS / IDLE_TTL_SECS` 的全部读取(随 warmpool 包删除消失)。
- 检查 `.env.example` / deploy compose / 文档是否出现这些变量并清理。

## 4. 删除清单(文档)

- **新增 ADR-0016**「移除 warm pool 能力」:Status Accepted;Supersedes ADR-0012;
  Amends ADR-0015(推翻其备选 D);陈述"永久不兼容 + 中小并发冷启可接受 + 维护负担 +
  未来要的是另一套机制"四点理由;Consequences 列出代码删除面与"不再保留缝口"的取舍。
- **ADR-0012**:Status 改 `Superseded by ADR-0016`,文首补一句指向。
- **ADR-0015**:在 Decision/Consequences 补注"warm pool 已由 ADR-0016 移除;本 ADR
  关于'保留可选'的部分由 0016 收敛",策略表态(默认按需分配)仍有效。
- **ADR-0008 §3**:warm-pool 段补"最终由 ADR-0016 移除该能力"。
- **ADR-0009 / 0013 / 0014**:warm pool 提及处补一句指向 ADR-0016(轻量,不重写)。
- **docs/adr/README.md**:0012 行 Status 改 Superseded;新增 0016 行。
- **docs/plan/hardening-warm-pool.md**:文首加"已废弃 —— 见 ADR-0016,warm pool
  能力已移除"横幅(保留作历史)。
- **README.md 路线图**:WP 行状态由"✅(引擎,默认关闭)"改为"已移除(ADR-0016)",
  GV 行里"承接 K8s warm-pool 节点镜像预拉"措辞清理。

## 5. 校验与提交

1. `cd apps/sandbox-manager && GOWORK=off go build ./... && GOWORK=off go vet ./... &&
   GOWORK=off go test ./...` 全绿。
2. `gofmt -l` 干净。
3. `grep -rin "warm\|adopt" apps/sandbox-manager --include=*.go` 仅剩 opensandbox.go
   里与 warm 无关的 "connection pool" 等措辞。
4. 写 `docs/archive/` changelog(背景 / 删除面 / 校验)。
5. 单次提交(不用 `--no-verify`,不提交 `.claude/`),push。

## 6. 风险与回滚

- **风险**:误删到按需主路径或 metrics 的 hit/miss。**缓解**:metrics 仅删 pooled
  维度,hit/miss/active/create_p99 全留;binder 慢路径除去 adopt 分支外逐字不动,
  靠现有 binder 单测兜底。
- **指标兼容**:保留 `cocola_sandbox_pool_*` 指标名,既有看板/告警不破。
- **回滚**:纯删除型改动,`git revert` 单个 commit 即可恢复(代码仍在 git 历史,
  ADR-0015 备选 D 的"将来重写"判断也已写进 ADR-0016,无信息丢失)。
