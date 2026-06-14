# Plan: M8 可观测性与压测 —— 让 cocola 可被观测、可被压、可定容量

> 状态:**待评审**(2026-06-14)。本文先定方案与开源选型,经确认后再分步实现。
> 设计原则(承 ADR-0001):复用成熟开源,**不造轮子**;Go 静态二进制热路径零额外
> 重型依赖;Python 服务用社区标准 instrumentator;一套指标贯穿三语言。

## 1. 目标与动机

M0–M7 已把功能链路打通,但当前**几乎不可观测**:

- 唯一的 metrics 是 `sandbox-manager/internal/orchestrator/metrics.go` —— 一个
  进程内、零依赖的内存 sink(hit_rate / create_p99 / active_count),其注释
  本就写明「A Prometheus/OTel exporter can wrap Snapshot() later」。它**不暴露
  任何 HTTP 端点**,重启即丢,跨副本不可聚合。
- 三个 Go 服务(gateway / sandbox-manager / admin-api)、两个 Python 服务
  (agent-runtime / llm-gateway)**都没有 /metrics 端点**,没有统一的请求延迟 /
  错误率 / 在途并发指标。
- 日志虽有 `go-common/logger`(zap)与 Python logging,但**无 trace_id 贯穿**,
  一条用户请求跨 gateway→agent-runtime→sandbox-manager→llm-gateway 时无法串联。
- **没有任何压测基线**:不知道单沙箱冷启 p99、网关 SSE 吞吐、绑定命中率在压力
  下的表现,也就无法回答「预热池该预热几个」(M6+ warm pool 的前置依赖)。

M8 目标:让 cocola **可观测(metrics + 结构化日志 + 链路追踪)** 且 **有压测基线
与容量结论**,为后续 warm pool / 扩容决策提供数据支撑。

## 2. 范围

### 做(本里程碑)

1. **Metrics(三支柱之一,优先级最高)**
   - 新增 `packages/go-common/metrics`:基于 Prometheus `client_golang`,提供
     统一 registry、HTTP `/metrics` handler、以及 RED 中间件(gRPC 拦截器 +
     net/http 中间件)。三个 Go 服务复用同一包。
   - `sandbox-manager`:把现有 `orchestrator.Metrics`(内存 sink)**桥接**到
     Prometheus —— 用 collector 包一层 `Snapshot()`,保留原 API 与单测不破坏
     (ADR/注释里早已预留)。新增 RED 指标(gRPC 方法级 QPS/延迟/错误)。
   - 两个 Python 服务:用 `prometheus-fastapi-instrumentator`(社区标准,一行
     接入 FastAPI)暴露 `/metrics`;agent-runtime 的 gRPC 侧用
     `py-grpc-prometheus` 或自定义 interceptor。
2. **结构化日志 + trace 关联**
   - 统一日志字段:`trace_id` / `span_id` / `service` / `session_id`。Go 侧
     扩展 `go-common/logger`(zap)注入 trace 字段;Python 侧统一 logging 格式。
   - trace_id 在入口(gateway BFF)生成或透传(W3C `traceparent` header),
     经 gRPC metadata 向下游传播。
3. **Tracing(分布式链路,基于 OpenTelemetry)**
   - 引入 OTel SDK(Go: `go.opentelemetry.io/otel`;Py: `opentelemetry-sdk`
     + 自动 instrumentation for FastAPI/gRPC),导出到 OTLP collector。
   - 默认采样率低(如 5%)且**可完全关闭**(env 开关),零配置时不影响性能。
4. **部署物(可选启用的观测栈)**
   - `deploy/observability/`:Prometheus(scrape 五个服务 + k8s SD)、Grafana
     (预置 dashboard JSON:RED + 沙箱生命周期 + 绑定命中率 + 配额/计费)、
     可选 OTel Collector + Tempo/Jaeger。
   - `docker-compose.observability.yml`:本地一键起观测栈(对齐 M6 的 k3d/本地
     友好路线),k8s 侧给 Helm values 开关。
5. **压测(load testing)**
   - HTTP/SSE 链路:用 **k6**(开源,脚本即代码)压 gateway BFF 的对话/SSE。
   - gRPC 链路:用 **ghz**(开源)压 sandbox-manager 的 Acquire/Exec、
     agent-runtime 的 Query。
   - `bench/`:放 k6/ghz 脚本 + 一份「容量基线报告」runbook(冷启 p99、绑定
     命中率随并发变化、SSE 吞吐、错误率拐点)。

### 不做(后续里程碑)

- **告警(alerting)规则上生产**:M8 先给 dashboard 与基线,Alertmanager 规则
  与 SLO 定义留作 M8+/运营阶段。
- **预热资源池(warm pool)**:M8 只产出其所需的容量数据,实现仍在 M6+。
- **日志集中检索栈(Loki/ELK)**:本地用 stdout + compose 足够;集中检索按需。
- **生产级长期存储**(Thanos/Mimir/Cortex):自托管单机用 Prometheus 本地 TSDB
  即可,海量多集群留后续。

## 3. 开源选型(复用,不造轮子)

| 关注点 | 选型 | 理由 |
|---|---|---|
| Go metrics | `prometheus/client_golang` | 事实标准,RED/直方图齐全,静态二进制友好 |
| Go gRPC 指标 | `grpc-ecosystem/go-grpc-middleware` (prometheus) | 拦截器一把梭,社区主流 |
| Python metrics | `prometheus-fastapi-instrumentator` | FastAPI 一行接入,自带 RED |
| Python gRPC 指标 | `py-grpc-prometheus` 或自写 interceptor | 轻量 |
| Tracing | OpenTelemetry(OTel)SDK + OTLP | 跨语言统一、厂商中立、可换后端 |
| Trace 后端 | Tempo 或 Jaeger(可选) | 自托管友好,二选一给开关 |
| 指标存储/可视化 | Prometheus + Grafana | 自托管事实标准 |
| HTTP/SSE 压测 | k6 (Grafana) | 脚本即 JS,SSE/阈值/报告齐全 |
| gRPC 压测 | ghz | gRPC 专用,直方图/CSV 输出 |

> 全部为 CNCF/Grafana 生态主流开源,自托管零授权成本,契合 cocola 定位。

## 4. 关键设计点 / 风险

1. **不破坏现有 orchestrator.Metrics**:它有单测(binder_test 等)依赖其 API。
   方案是**新增一个 Prometheus collector 适配层**读取 `Snapshot()`,而非改写
   它——遵循 ADR-0002 式「加 seam 不动核心」的一贯做法。
2. **/metrics 端点与 gRPC 端口共存**:Go 服务当前只 `grpc.Serve`。需要再起一个
   轻量 `net/http` 监听(独立端口,如 `:9090`)暴露 `/metrics` 与 `/healthz`。
   **遵守 <network_security>:本地 sandbox 内绝不起监听端口**——这些 HTTP 监听
   只在真实部署的容器/集群里跑,不在本地 bash 沙箱里跑;单测用 httptest 不绑真实
   端口。
3. **性能开销可控**:Prometheus 抓取是 pull,开销极小;OTel 默认低采样且可 env
   关闭。务必保证「不开观测栈时零额外延迟」。
4. **trace 传播跨语言**:统一用 W3C traceparent + OTel propagator,避免自造头。
5. **压测安全**:压测只打本地/测试集群,不打生产;脚本参数化目标地址,默认指向
   compose/k3d。

## 5. 实施步骤(每步:实现 + 单测 + changelog,hooks 全过)

- **S1 — go-common/metrics 基座**:新增包(registry + /metrics handler + RED
  中间件/拦截器)+ 单测(httptest 抓 /metrics 断言指标存在)。
- **S2 — Go 服务接入**:gateway / sandbox-manager / admin-api 各起 observability
  HTTP 端口,挂中间件;sandbox-manager 再加 orchestrator.Metrics 的 collector
  桥接。单测覆盖桥接与端点。
- **S3 — Python 服务接入**:llm-gateway / agent-runtime 接
  prometheus-fastapi-instrumentator + gRPC interceptor,暴露 /metrics。pytest。
- **S4 — 日志 trace 字段 + OTel 链路**:logger 注入 trace_id;入口生成/透传
  traceparent;OTel SDK + OTLP 导出(env 开关,默认低采样)。
- **S5 — 部署物**:deploy/observability/(Prometheus + Grafana dashboard JSON +
  可选 OTel Collector + Tempo/Jaeger)+ docker-compose.observability.yml +
  Helm values 开关。
- **S6 — 压测**:bench/ 放 k6(SSE)+ ghz(gRPC)脚本 + 容量基线 runbook;
  在 k3d/compose 上跑出首版基线数据,写入 runbook。

> 步骤可独立交付:S1–S3(metrics)是最小可用增量,先落地即能看板化;S4(trace)
> 与 S5/S6 可后续追加。建议先做 S1–S3 验证价值,再推进 S4–S6。

## 6. 验收标准

- 五个服务都暴露 `/metrics`,Prometheus 能抓到 RED 指标;Grafana 看板可见
  QPS/延迟/错误率、沙箱生命周期、绑定命中率、配额/计费。
- 一条用户请求在 trace 后端可见跨 gateway→agent-runtime→sandbox-manager→
  llm-gateway 的完整 span 链(开启 tracing 时)。
- 不开观测栈时,服务行为与延迟与 M7 一致(零回归)。
- bench/ 能一键压测并产出基线报告:冷启 p99、绑定命中率随并发变化、SSE 吞吐、
  错误率拐点。
- 全量单测 + gofmt/ruff + hooks 全绿;每步配 docs/archive/ changelog。

## 7. 产出物清单

- `packages/go-common/metrics/`(新包)
- 各 Go 服务 observability HTTP 端点接线
- 两个 Python 服务 /metrics + interceptor
- `go-common/logger` trace 字段扩展 + OTel 接入
- `deploy/observability/` + `docker-compose.observability.yml` + Helm values
- `bench/`(k6 + ghz 脚本 + 基线 runbook)
- 对应 ADR(新增 `docs/adr/0011-observability-and-load-testing.md`,记录三支柱
  选型与开关策略)
