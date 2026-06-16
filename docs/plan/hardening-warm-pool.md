# Plan: Warm Pool 预热池 —— 隐藏 Route-A 沙箱冷启动延迟

> 状态:规划中(2026-06-16)。本文是落地方案,**先评审、未动代码**。
> 关联:ADR-0008 §3「Warm pool」、§4 与 Consequences「cold start the thing the
> warm pool must hide」;ADR-0009 Consequences「mitigated by lazy-start +
> hibernate + warm-pool」;ADR-0002 Provider 抽象铁律;任务 #13。

## 1. 目标与动机

cocola 走 Route-A(大脑 + 凭据都在沙箱内,见 ADR-0009),沙箱镜像很重
(Node + Claude Code + Python + uv,K8s 形态约 1.9GB)。新会话首次执行时要
**现拉镜像 / 现起容器**,这段延迟直接压在用户首响应上。warm pool 的职责就是把
这段「镜像拉取 + 容器启动」从请求路径里挪走:后台预先备好一批已就绪的空闲沙箱,
新会话来了直接「领用」一个,而不是「现造」。

### 1.1 #14 实测冷启数据(本机 Docker provider,EchoProvider 框架基线)

| 路径 | 首响应延迟 | 说明 |
|---|---|---|
| 复用同 session(稳态) | ~0.62–0.67s | 沙箱已绑定,纯框架转发开销 |
| 新 session(冷启) | ~1.0–1.16s(均值 ~1.08s) | 含现起沙箱 |
| **冷启净增量** | **≈0.44s / req** | 即 warm pool 能抹掉的部分 |

> 数据来源:`bench/README.md` §3.2(#14 回填)。本机 Docker 镜像已在本地、
> 容器轻量,所以净增量只有 ~0.44s。**K8s + gVisor 形态会显著更重**:1.9GB 镜像
> 首拉 + `runsc` 沙箱启动,冷启会从「亚秒」升到「数秒」量级——这正是 ADR-0008/0009
> 把 cold start 列为「warm pool must hide」的原因。真实数字待 #15 集群复测校准。

**结论**:本机数据已足够论证「冷启净增量真实存在且随 backend 变重而放大」,足以
立项 warm pool;但**预热个数 / 容量参数必须等 #15 集群实测**才能定。本 Plan 据此
分层:把「与 backend 无关的池化编排逻辑」在本机做完做透(fake provider 全量单测),
把「真实冷启收益与容量调参」标为待集群验收门。

## 2. 设计原则:复用,不造轮子

warm pool 是成熟模式,**不自研调度器**,复用既有结构与业界实践:

- **复用 cocola 现有缝口**:池化只在 `Binder` 的 slow-path「该 Create 了」这一处
  插入一个「先试领用、领不到再 Create」的分支;`SandboxProvider` 接口
  **零改动**(ADR-0002 铁律),Docker/K8s provider 不感知池子的存在。
- **复用业界语义**(ADR-0008 §3 已锚定的「reference practice」):池子分层为
  `idle pool → bind on demand → return/destroy → async refill`,空闲 cooldown
  ~30min(活动续期)、硬上限 lifetime ~2 天。这与 Knative「Pod 预留 + scale-from
  -zero」、E2B/Firecracker「pre-warmed sandbox pool」、数据库连接池
  (HikariCP min-idle + max-pool + 后台补池)的形态一致——都是「最小空闲数 + 上限
  + 异步补池 + 老化回收」四件套。我们照搬这套参数模型,不发明新机制。
- **复用 Redis 作为池状态存储**:binder/lease/lock 已全部走
  `packages/go-common/redis` 的 KV,池的「空闲清单 / 在制数」继续放 Redis,
  天然支持多副本 sandbox-manager 共享一个池、避免各自为政。

## 3. 现状勘定(已查代码)

| 缝口 | 位置 | 现状 |
|---|---|---|
| Provider 接口 | `internal/provider/provider.go` | `Create/Exec/Pause/Resume/Destroy/...`;`Register/Get` 注册表 |
| 绑定核心 | `internal/orchestrator/binder.go` `AcquireWithOutcome` | fast-path 复用 → slow-path 取锁 → `p.Create` → `bind` 写双向映射+lease |
| 冷启计时 | 同上 `b.recordMiss(time.Since(start))` | 已埋 miss 延迟指标(M8) |
| 生命周期配置 | `Config` / `ConfigFromEnv` | `COCOLA_SANDBOX_*` 秒级 env 覆盖 + `withDefaults()` |
| 网络策略 | `b.net` (`WithNetworking`) | 每次 Create 注入 egress allowlist |
| 回收 | `internal/orchestrator/reaper.go` | 空闲超期沙箱 GC(已有) |

**关键观察**:slow-path 里 `p.Create(...)` 是**唯一**新建沙箱的地方。warm pool
只需在「双重检查未命中、即将 Create」之前插一步「向池子领一个」,领到则跳过
Create、改走「重打绑定标签」;领不到则原样 Create(优雅降级)。**改动面极小、
且完全收敛在 orchestrator 包内**。

> 一个语义难点:Route-A 沙箱要挂 per-user volume(`SandboxSpec.UserID`),而预热时
> 还不知道哪个用户来领。处理见 §4.2——预热的是「用户无关的空白沙箱骨架」,领用时
> 再挂用户卷 / 注入会话级 env;若某 provider 无法「后挂卷」,则池只对该 backend
> 降级为「预拉镜像 + 预热节点」而非「预起整箱」。本机 Docker 用 bind-mount,可后挂;
> K8s 卷在 Pod 创建时绑定,故 K8s 形态的池化策略需在 #15 单独定档(见 §6 风险)。

## 4. 设计

### 4.1 组件:`internal/orchestrator/warmpool`(新包)

一个与 `Binder` 解耦的小组件,职责单一:维护一批 ready 沙箱,提供
`Checkout()`(领一个,无则返回 miss)与后台 `refill loop`(补到 min-idle)。

```
type Pool interface {
    // Checkout 尝试领一个就绪沙箱;池空返回 (nil, false, nil),由调用方降级为 Create。
    Checkout(ctx) (*provider.Sandbox, bool, error)
    // Size 返回当前空闲数(供 metrics / 调参)。
    Size(ctx) (int, error)
}
```

- **Provider 注入**:Pool 持有同一个 `provider.SandboxProvider`,用「用户无关的
  预热 spec」(空 UserID / 占位 SessionID / 统一 Image + egress)调 `Create`。
- **存储**:空闲沙箱 ID 清单 + 在制计数放 Redis(复用 `rds.KV`),多副本共享。
- **补池**:后台 goroutine,`current_idle + in_flight < minIdle` 时异步补;受
  `maxPool`(总量上限,防雪崩)与 per-tick 补池配额约束。
- **老化**:预热沙箱也有 createdAt;超过 hard max lifetime 或长时间未被领用,
  由 reaper(复用现有 `reaper.go` 逻辑或并入)Destroy 后再补,防止池里堆「僵尸热箱」。

### 4.2 接入点:`Binder.AcquireWithOutcome` slow-path

仅改一处。双重检查未命中后,原本直接 `p.Create`,改为:

```
// 1. 先试从池领用并 adopt(仅当 provider 实现 optional provider.Adopter)
if sb, pooled := b.tryAdopt(ctx, spec); pooled {
    // tryAdopt 内部:Checkout 一个热箱 → adopter.Adopt(挂用户卷/注会话 env)
    //               → 改写 UserID/SessionID 归属。任一步失败则销毁孤儿并返回
    //               pooled=false,落到下面的冷 Create。
    ... bind + recordPooled() ...
    return Outcome{Sandbox: sb, Reused: false /*新 bind,但来自池*/}, nil
}
// 2. 池空 / 未启用 / provider 不可 adopt / adopt 失败 → 优雅降级,原样 Create
sb, err := b.p.Create(ctx, ...)   // 现状逻辑,一行不删
```

- **优雅降级是硬要求**:池为空 / Pool 未启用 / Checkout 出错,**一律回落到原
  Create 路径**,行为与今天完全一致。warm pool 是纯优化,绝不能成为新故障点。
- **开关**:`COCOLA_WARMPOOL_ENABLED`(默认关),`COCOLA_WARMPOOL_MIN_IDLE` /
  `COCOLA_WARMPOOL_MAX` / cooldown / lifetime 全部 env 可调,沿用 `ConfigFromEnv`
  风格。默认关 = 现有部署零行为变化。

### 4.3 「领用即归属」(adopt)是 provider 的 **optional 能力**

adopt = 把一个用户无关的预热箱**重新归属**到具体 user/session(挂该用户的卷、
注会话 env)。这是个**可选**的 provider 能力,定义为 `provider.Adopter` 接口:

```
type Adopter interface {
    Adopt(ctx, sid string, spec SandboxSpec) error
}
```

binder 在 slow-path 用 `b.p.(provider.Adopter)` 做类型断言:**实现了才走池,没实现
则热箱不可领用、一律冷 Create**。这样核心 `SandboxProvider` 接口零改动(ADR-0002),
能否池化是各 backend 自己的事。

| Provider | 能否 adopt | 现状 / 计划 |
|---|---|---|
| Docker | **否** | bind-mount 在 `ContainerCreate` 时固定,运行中容器无法热挂用户卷;Docker 冷启净增量本就只有 ~0.44s(#14),收益最小。**不实现 Adopter**,开池时 binder 自动跳过池、照常冷 Create。 |
| K8s + gVisor | **是(待 #15)** | warm pool 的真正价值场景(1.9GB 镜像 + runsc,冷启数秒级)。adopt 的卷绑定时机与 #15 的 PVC 绑定问题是**同一个**,合并到 #15 一并定档实现。 |
| fake(单测) | 是 | 测试用 `adoptableProvider` 包装 fakeProvider,驱动 binder 的池路径。 |

> **重要修正(2026-06-16)**:原 Plan 写「Docker 后挂卷」是过度承诺——核对
> `provider/docker/docker.go` 后确认 Docker 不支持运行中热挂 bind-mount。强行绕开
> (挂 userdata 根 / cp 拷贝)会破坏数据隔离或 ADR-0008 的持久化语义,与「复用、不
> 造轮子」相悖。因此 Docker 明确**不**做 adopt;真实 adopt 实现 + 冷启实测随 K8s
> provider 落在 #15。本次只交付 **backend 无关的池引擎**(S1)+ **binder 接缝**(S2)。

## 5. 落地分层(对齐 m6/vault Plan 的「本机可完成 + 待集群验收门」范式)

- **S1 池核心(本机可完整测试)✅ 已完成**:新增 `warmpool` 包 + `Pool` 接口 +
  Redis 状态(idle marker + Get-then-Del 无锁 CAS 领用)+ 补池/老化 loop +
  `ConfigFromEnv`;**fake provider + fake KV** 全量单测(disabled no-op、补池收敛
  MinIdle、钳 Max、领用收缩、空池 miss、并发领用不超卖、lifetime 老化回收、默认值)。
- **S2 binder 接入(本机可完整测试)✅ 已完成**:slow-path 加 `tryAdopt`(经
  optional `provider.Adopter` 接口),任一步失败优雅降级回 Create;`pooled` 指标;
  binder 契约测试(池关=逐字同冷启、可 adopt 走池零 cold-create、空池降级、adopt
  失败销毁孤儿+降级、非 Adopter provider 跳过池)。main 装配 `COCOLA_WARMPOOL_*`
  开关(默认关)。
- **~~S3 Docker adopt~~ 取消**:核对代码后确认 Docker 无法运行中热挂卷(见 §4.3
  修正),且 Docker 冷启收益最小。改为「Docker 不实现 Adopter,开池时自动跳过」。
- **S4 部署物(本机编写,真链路待集群)**:`deploy/k8s` / helm 加 warmpool env 开关
  样例与文档;`helm template` 静态校验。(可与 #15 合并)
- **S5 验收门(并入 #15 集群)**:K8s provider 实现 `provider.Adopter`(卷绑定时机
  与 #15 的 PVC 绑定问题同源,一并定档:整箱预热 vs 镜像/节点预热);用真实重镜像
  + runsc 实测冷启收益、据此定 min-idle/max 容量参数;回填 `bench/README.md`;在
  ADR-0008 Follow-ups 收口。

S1、S2 各独立 commit,各带 `docs/archive/` changelog;Plan 修正随 S1 一并提交。

## 6. 风险与权衡

- **卷绑定时机决定 adopt 可行性**:Docker 的 bind-mount 在 `ContainerCreate` 固定
  → **Docker 不做 adopt**(已确认,见 §4.3)。K8s PVC 同样在 Pod 创建期绑定,「预起
  空 Pod 再后挂用户卷」不天然成立;#15 在集群上二选一定档——(a)池只预热到「镜像
  已拉 + 节点就绪」,Pod 仍按需起(省掉最大头镜像拉取);(b)探索 generic ephemeral
  volume / 延迟绑定让 Pod 起后再 attach。**不在本机猜测,留给 #15 实测。**
- **资源占用**:预热箱空转吃 CPU/RAM(#14 观察 Docker Paused 态约 3.4MiB/个,
  但 K8s + Node 进程会重得多)。用 `maxPool` 上限 + 老化回收 + 默认关闭 控制成本;
  容量参数等 #15 实测定。
- **池污染**:领用过的箱不回池(用完即按 lease/reaper 销毁),池只装「全新预热箱」,
  避免跨会话数据残留——安全优先于复用率。
- **预热 spec 的 egress**:预热箱也必须带 egress allowlist(`b.net`),不能出现
  「预热期裸奔」窗口。adopt 时若用户策略不同需可覆写。
- **指标真实性**:#14 发现 Prometheus 对服务端口 up=0(cross-network scrape 缺口),
  warm pool 收益验证以 k6/ghz 自报延迟为准,与 #14 口径一致。

## 7. 验收标准

- 开关默认关时,全链路行为与当前**逐字一致**(契约测试保证)。
- 开池 + Docker 本机:新 session 首响应延迟从 ~1.08s 降至接近复用稳态 ~0.64s,
  净增量 ~0.44s 基本被抹平(S3 实测回填 bench/README.md)。
- 池空 / Checkout 失败时优雅降级,不产生任何新失败模式。
- 并发领用不超卖、补池收敛到 min-idle、老化箱被回收(S1 单测)。
- K8s 形态收益与容量参数在 #15 集群实测后于 ADR-0008 收口。
