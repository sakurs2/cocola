# 变更记录：S1 —— warm-pool 引擎(backend 无关)

- 关联 Plan：`docs/plan/hardening-warm-pool.md`（S1 阶段）
- 关联 ADR：ADR-0008 §3「Warm pool: idle pool → bind on demand → return/destroy
  → async refill」
- 日期：2026-06-16

## 背景

#14 容量基线显示 Route-A 每次新会话冷启动相对复用净增约 0.44s/req(Docker
EchoProvider:复用 ~0.62–0.67s vs 新会话冷启 ~1.0–1.16s);K8s+gVisor(1.9GB
镜像 + runsc)会更重(秒级)。预热池的目标是把这段冷启动从请求关键路径上挪走:
后台维持一批 user-agnostic 的空闲沙箱,新会话来时直接领用并改挂,而不是现 create。

## 复用不造轮子

引擎完全复用既有基础设施,不引入新依赖:
- `rds.KV` 原语(Get/Set/Del/ScanKeys)做空闲池存储与领用,**不写新 Lua**:
  空闲沙箱各存一个可扫描 key(`cocola:sb:warm:idle:{id}`),领用用 Get-then-Del
  CAS——`Del` 返回计数 ==1 即原子单赢家,在真 Redis 与 Fake 上行为一致。
- 形态对标 HikariCP(min-idle + max + 异步补池 + age-out)、E2B / Knative
  pre-warmer:空闲下限 `MinIdle`、硬上限 `Max`、定时异步补池、超龄回收。
- `SandboxProvider` 核心接口(ADR-0002 铁律)**零改动**:引擎只依赖
  `Create/Destroy`,领用改挂的能力由后续 S2 的可选 `provider.Adopter` 缝口承载。

## 设计

`warmpool.Pool` 是 backend 无关的纯引擎:
- `Checkout(ctx)`:ScanKeys 扫空闲 key → Get-then-Del CAS 领用;空池 / 出错均
  返回 `(nil, false, nil)`——绝不制造新失败模式,调用方静默降级到 cold Create。
- `Run(ctx)`:disabled 即空跑;否则 ticker 周期 `tick`(先 age-out 超龄,再补池)。
- `refill`:补到 `idle + inflight >= MinIdle`,以 `Max` 为硬上限;`warmOne` 以
  user-agnostic 方式 Create(SessionID `warm-<token>`、无 UserID),发布失败即
  Destroy 不泄漏。
- `ageOut`:领用并销毁超过 `MaxLifetime`(默认 48h)的陈旧沙箱。
- `ConfigFromEnv`:`COCOLA_WARMPOOL_ENABLED/MIN_IDLE/MAX/REFILL_SECS/
  MAX_LIFETIME_SECS/IDLE_TTL_SECS`;`withDefaults` 以 `Max` 为天花板钳制
  `MinIdle`。

## 改动

- `apps/sandbox-manager/internal/orchestrator/warmpool/pool.go`(新增):引擎本体。
- `apps/sandbox-manager/internal/orchestrator/warmpool/pool_test.go`(新增):9 项
  单测,含 disabled 空跑、补池收敛到 MinIdle、补池不超 Max、领用收缩、空池 miss、
  12 并发领 5 箱恰好 5 个不同(无双发)、注入时钟的 age-out 回收、warm 失败不泄漏、
  withDefaults 钳制。
- `docs/plan/hardening-warm-pool.md`(新增):#13 计划;含 §4.3「Docker 不可
  adopt」修正说明与 S3 取消 / S5 并入 #15 的分层调整。

## 验收

- `apps/sandbox-manager`:`go build ./...` + `go vet ./...` 干净(GOWORK=off)。
- `go test ./internal/orchestrator/warmpool/...` 全绿。

## 范围说明

S1 仅 backend 无关引擎(本机可完整测试)。S2(binder 接入 + `provider.Adopter`
缝口 + main 装配)随后独立提交。真实领用改挂(K8s volume 重挂)与冷启动实测随
#15(gVisor / K8s provider)落地——Docker 因 bind-mount 固定在 ContainerCreate
无法对运行中容器后挂,故不实现 Adopter。
