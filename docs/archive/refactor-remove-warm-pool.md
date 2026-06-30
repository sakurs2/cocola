# Changelog: 移除 warm pool 能力(代码 + 文档)

日期: 2026-06-30
关联: task #42-#46、ADR-0016(新增)、ADR-0015(修订)、ADR-0012(superseded)、ADR-0008 §3、ADR-0009、ADR-0013、ADR-0014

## 背景
ADR-0015 在 OpenSandbox-only 语境下已确立「按需冷启动分配」为默认且唯一主路径,
warm pool 降为默认关闭的可选优化、缝口原地保留。但实测 OpenSandbox 运行中沙箱 API
仅暴露 metadata/pause/resume/renew-expiration/snapshots/proxy,无任何增删卷端点,
volumes 仅创建时可指定 —— adopt-by-remount(预热整箱、领用后挂用户卷)在唯一主后端
永久不可行。继续保留 Pool/tryAdopt/Adopter 缝口只剩维护负担而无任何后端可兑现收益。
故按 ADR-0016 将 warm pool 能力从代码与文档中整体移除;未来若需隐藏冷启动延迟,
方向是「按 (user,session) 预测性预创建」,属不同机制,不复用本引擎。

## 改动
### docs/adr/0016-remove-warm-pool-capability.md(新增)
- Status: Accepted;Supersedes ADR-0012;Amends ADR-0015。
- 四点理由:OpenSandbox 永久不兼容 adopt-by-remount;自建中小并发冷启动可接受;
  维护缝口的负担;未来预测性预创建是不同机制。Alternatives A/B/C + Consequences。

### docs/plan/remove-warm-pool.md(新增)
- 移除前的 Plan 文档:背景动机、范围与非目标、代码删除清单、文档清单、验收、风险回滚。

### 代码删除
- 删除整个 `internal/orchestrator/warmpool/` 包(pool.go + pool_test.go,含 redis `warm:` 命名空间)。
- 删除 `internal/orchestrator/warmpool_binder_test.go`。
- `internal/orchestrator/binder.go`:移除 warmpool import、`pool` 字段、`WithWarmPool`、
  `AcquireWithOutcome` 中的 warm-pool fast-adopt 分支(慢路径直接 `b.p.Create`)、
  `tryAdopt`、`recordPooled`。
- `internal/provider/provider.go`:移除可选 `Adopter` 接口及其文档;核心 8 方法接口不动。
- `internal/orchestrator/metrics.go`:移除 `pooled` 字段、`recordPooled`、`Snapshot.PooledCount`;
  顶部注释 `pool_hit_rate` → `reuse_hit_rate` 文案。
- `cmd/sandbox-manager/main.go`:移除 warmpool import;warm pool 构造与 `pool.Enabled()`
  分支替换为仅注释说明「按需分配唯一路径(ADR-0015/0016)」;binder 接线改为
  `NewBinder(kv,p,cfg).WithMetrics(bm).WithNetworking(net)` + `go binder.RunReaper(ctx)`。
- `internal/orchestrator/binder_opensandbox_live_test.go`:移除 `provider.Adopter` 断言块;
  更新头注释为「on-demand cold-start 唯一分配路径(warm pool removed in ADR-0016)」。

### 指标语义(名称保留,仅改 HELP 文案)
- `internal/obs/collector.go` + `collector_test.go`:HELP「reused a warm sandbox」→
  「reused their existing sandbox」;hitRate help →「Session->sandbox reuse rate」。
  指标名 `cocola_sandbox_pool_hit_rate/_hits_total/_misses_total` 不变,避免破坏现有看板与告警。

### 旧 ADR 标注与文档清理
- ADR-0012:Status → `Superseded by ADR-0016`;文末追加 Superseded 段。
- ADR-0015:Status 行标注;文末追加 Amendment 段(否决项 D「删 warm pool」改为采纳)。
- ADR-0008 §3:追加「Finally removed by ADR-0016」。
- ADR-0009:lazy-start+hibernate 段标注「warm-pool 部分已由 ADR-0016 移除」。
- ADR-0013/0014:warm pool 相关条目标注「已由 ADR-0016 移除」。
- docs/adr/README.md:0012 行 → 「Superseded by 0016」;补齐缺失的 0015 行 + 新增 0016 行。
- docs/plan/hardening-warm-pool.md:顶部加「⛔ 已废弃(2026-06-30)」横幅。
- README.md:路线图 WP 行 →「已移除(ADR-0016)」;GV 行去除 K8s warm-pool 节点镜像预拉表述。

## 验收
- `GOWORK=off go build ./...` 绿。
- `GOWORK=off go vet ./...` 绿。
- `GOWORK=off go test ./...` 全绿。
- gofmt 干净;repo 全局 `grep "orchestrator/warmpool"` 无残留 import。

## 不含
- 指标名变更(刻意保留,避免破坏 Grafana 看板/告警)。
- `COCOLA_SANDBOX_IMAGE`(Route-A brain image,非 warm-pool 物,保留)。
