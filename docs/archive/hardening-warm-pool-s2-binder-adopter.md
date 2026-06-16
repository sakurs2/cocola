# 变更记录：S2 —— binder 接入 + `provider.Adopter` 缝口

- 关联 Plan：`docs/plan/hardening-warm-pool.md`（S2 阶段）
- 关联 ADR：ADR-0002（SandboxProvider 抽象铁律,核心接口不动)、ADR-0008 §3
- 日期：2026-06-16

## 背景

S1 落地了 backend 无关的预热池引擎。S2 把它接到编排关键路径上:新会话 miss 时
优先从池里领用一个预热沙箱并改挂到当前 user/session,而不是 cold Create。难点
是「领用改挂」会改变沙箱身份,而核心 `SandboxProvider` 接口(ADR-0002)不允许动。

## 设计

新增**可选能力接口** `provider.Adopter`——把改挂能力放在核心接口之外,谁能改挂
谁实现:

    type Adopter interface {
        Adopt(ctx context.Context, sid string, spec SandboxSpec) error
    }

binder 在 `AcquireWithOutcome` 慢路径、cold Create 之前插入 `tryAdopt`:
- 池为 nil / disabled → 返回 miss;
- provider 未实现 `Adopter`(`b.p.(provider.Adopter)` 断言失败)→ 返回 miss;
- `Checkout` 空池 / 出错 → 返回 miss;
- `Adopt` 失败 → 销毁这个已出池的孤儿沙箱,返回 miss。

以上**任一失败都静默降级到正常 cold Create**——预热池是纯延迟优化,永不成为新
失败模式。adopt 成功后改挂 UserID/SessionID 并走正常 bind;bind 失败则销毁孤儿
再回落 cold Create。

main 装配:`COCOLA_WARMPOOL_ENABLED` 开启时 `go pool.Run(ctx)` 起后台补池/回收
循环;若 provider 不实现 `Adopter` 则打 warn(预热箱不会被使用)。

## Docker 不实现 Adopter(关键修正)

原 Plan §4.3 曾承诺「Docker 起空白容器、领用时后挂 user volume」。核对
`provider/docker/docker.go` 后确认:bind-mount(userDir/sessDir/pluginDir/
claudeDir)固定在 `ContainerCreate` 时写入 `hostCfg.Mounts`,**运行中容器无法
后挂卷**。可行的绕法(挂 userdata 根 = 数据越权;cp 拷贝 = 重造轮子且破坏
ADR-0008 持久化语义)均被否决。叠加 Docker 冷启动收益最小(~0.44s),故 Docker
不实现 `Adopter`,其预热箱保持不可领用、每次 miss 照常 cold Create。真实领用改挂
(K8s volume 重挂)与冷启动实测随 #15 落地。

## 改动

- `apps/sandbox-manager/internal/provider/provider.go`:新增可选 `Adopter` 接口。
- `apps/sandbox-manager/internal/orchestrator/binder.go`:新增 `pool` 字段、
  `WithWarmPool` setter、`tryAdopt` 助手、慢路径 adopt 分支、`recordPooled` 包装。
- `apps/sandbox-manager/internal/orchestrator/metrics.go`:新增 `pooled` 计数与
  `Snapshot.PooledCount`。
- `apps/sandbox-manager/internal/orchestrator/binder_test.go`:fakeProvider 加
  `destroys` 计数(校验孤儿销毁)。
- `apps/sandbox-manager/internal/orchestrator/warmpool_binder_test.go`(新增):
  5 项集成单测——disabled 等同 cold、miss 时 adopt(预热 2 领 1、请求路径零
  create、再来复用)、空池降级 cold、adopt 失败销毁孤儿+降级 cold、非 Adopter
  provider 跳过池。
- `apps/sandbox-manager/cmd/sandbox-manager/main.go`:装配预热池;启用时起后台
  循环,provider 不可 adopt 时打 warn。

## 验收

- `apps/sandbox-manager`:`go build ./...` + `go vet ./...` 干净(GOWORK=off)。
- `go test ./internal/orchestrator/...` 全绿(binder + warmpool 集成)。

## 范围说明

S2 是 backend 无关缝口 + Docker 显式不参与。真实 `Adopter` 实现(K8s volume
重挂)、冷启动实测、预热池容量调参合并到 #15(gVisor / K8s provider)。
