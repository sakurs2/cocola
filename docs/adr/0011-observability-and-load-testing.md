# ADR-0011: 可观测性三支柱与压测基线(M8)

- Status: Accepted
- Date: 2026-06-14
- Deciders: @cocola-maintainers

## Context

到 M7 为止,cocola 的五个服务(Go 的 gateway / sandbox-manager / admin-api,
Python 的 llm-gateway / agent-runtime)功能链路已经端到端打通,但**线上行为是
个黑盒**:没有统一指标,无法回答"现在 QPS 多少、P99 多高、错误率怎样";
没有跨服务的请求关联,一次会话从 BFF 经 gateway、agent-runtime 到 sandbox-manager
的调用链断成五截;也没有任何容量数据,谁都不知道单副本能扛多少并发、瓶颈在
LLM 上游还是沙箱冷启动。

M8 要补齐这块,且必须满足 cocola 的两条硬约束:

1. **复用开源、不造轮子。** 指标用 Prometheus 文本协议,链路用 OpenTelemetry,
   后端用 Grafana + Tempo——全是 CNCF 既成事实标准,不发明私有格式。
2. **沙箱内绝不监听端口**(见 `<network_security>`)。这影响指标如何暴露、
   链路如何导出:导出器只能是**客户端推**,不能是服务端听。

非功能约束:观测开销必须可控且默认安全——一个零配置的本地 dev boot 不应该
因为接了观测就变慢、变吵或强依赖一套外部后端;生产打开后成本要有上限。

本 ADR **不**处理:日志的集中采集与检索(沿用各服务 stdout 的结构化 JSON,
交给部署环境的日志栈);告警规则(留到观测栈稳定后单独沉淀);per-tenant 计费
口径(已在 ADR-0004 / M7 解决,与这里的 RED 指标正交)。

## Decision

按**三支柱**落地,指标与链路共用一套语义约定,默认关闭重型部分。

### 1. 指标(Metrics)——统一 RED 契约

- 所有服务发同一组指标名与标签,Go 与 Python 双实现严格对齐,这样**一套
  Prometheus + 一块 Grafana 看板覆盖全机队**:
  - `cocola_requests_total{service,transport,method,code}`
  - `cocola_request_duration_seconds{service,transport,method}`(共享分桶
    `0.001…30`,亚毫秒 RPC 到多秒沙箱冷启动同一直方图覆盖)
  - `cocola_requests_in_flight{service,transport}`
- `transport` 取 `http`/`grpc`;`method` 用**路由后的模板**(如
  `POST /v1/messages`、`/cocola.agent.v1.AgentRuntimeService/Query`),绝不用
  原始 path,避免标签基数爆炸。
- 每服务一个独立 registry(不用进程全局默认),测试因此天然隔离。
- 暴露遵守不监听约束:有 HTTP server 的服务把 `/metrics` 挂在自身 app 上
  (gateway、admin-api、llm-gateway);没有 HTTP server 的(sandbox-manager、
  agent-runtime)用一个**专用指标端口**,且该监听只在真实 composition root 里起,
  测试里永不绑定。
- Python 侧刻意**不用** prometheus-fastapi-instrumentator:它自带一套指标名,
  会让 Python 服务的看板和 Go 服务分叉。对齐 Go 的 RED 契约,值得多写约 40 行
  纯 ASGI 中间件(且该中间件不缓冲 body,保住 llm-gateway 的 SSE 流式)。

### 2. 链路(Tracing)——OTel + OTLP,默认关闭

- 统一用 OpenTelemetry SDK + **OTLP 导出器**。Go 侧用 OTLP/HTTP
  (`otlptracehttp`),Python 侧用 `opentelemetry-exporter-otlp-proto-http`;
  导出器是**客户端推**到 collector,不监听端口,满足沙箱约束。
- **默认 OFF。** 只有 `COCOLA_OTEL_ENABLED` 为真才建立导出管线。关闭时:
  - 仍然安装 W3C TraceContext + Baggage 传播器,这样上游若带了 `traceparent`,
    日志仍可关联;
  - 返回一个 no-op 的 stop 函数——不起 provider、不起导出器、不起 batcher,
    零额外开销。
- 打开时:`ParentBased(TraceIDRatioBased)` 头采样,默认比率 **0.05**(5%),
  批量导出,生产成本有上限。env 旋钮全机队一致:`COCOLA_OTEL_ENABLED`、
  `COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT`(默认 `localhost:4318`)、
  `COCOLA_OTEL_EXPORTER_INSECURE`(默认 true)、`COCOLA_OTEL_SAMPLER_RATIO`
  (默认 0.05)。
- 入口与出口自动埋点复用 contrib:Go 用 `otelhttp` / `otelgrpc`,Python 用
  `opentelemetry-instrumentation-fastapi` / `-grpc`,不手写 span。
- **日志关联**:logger 注入 `trace_id` / `span_id`——Go 用 `tracing.LogFields`,
  Python 用一个 structlog processor。无活跃 span 时不写这两个字段,dev 日志保持
  干净;OTel 一打开日志立刻可与 trace 互跳。

### 3. 部署栈与压测基线

- `deploy/observability/`:Prometheus 抓取配置(五服务)、Grafana 看板 JSON、
  可选 OTel Collector、Tempo;`docker-compose.observability.yml` 一键起本地栈;
  Helm values 提供开关。
- `bench/`:k6 跑 SSE(gateway 的 `/v1/messages` 流式),ghz 跑 gRPC
  (agent-runtime 的 `Query`);配套容量基线 runbook,记录单副本吞吐、P50/P99、
  瓶颈定位口径。

## Alternatives Considered

- **指标:OpenTelemetry Metrics 统一走 OTLP,而非 Prometheus 拉取。** 否决:
  Prometheus 拉模型在 K8s 里运维最成熟、生态最厚,且"挂 `/metrics`"比"推
  OTLP metrics"对沙箱不监听约束更自然(被抓取方只需在已有 HTTP server 上加一个
  路由)。链路用 OTLP 推、指标用 Prometheus 拉,是当前最省心的组合。

- **链路:默认开启 + 高采样。** 否决:与"零配置 dev 不应变重/强依赖后端"冲突,
  且生产全采样成本不可控。默认关闭 + 低采样,把"要不要观测、采多少"交给部署方,
  是更安全的缺省。

- **Python 指标:直接用 prometheus-fastapi-instrumentator。** 否决:它自带指标名,
  会让 Python 与 Go 的看板分叉,违背"一块看板覆盖全机队"的目标。

- **链路导出用 OTLP/gRPC 而非 HTTP。** 否决(Go 侧):自由解析 otel + otelgrpc
  会把 `go` 指令顶到 1.25 并拉高 grpc 版本;固定 otel `v1.28.0` 系列 + OTLP/HTTP
  让 workspace 模块的 `go` 指令稳在 **1.23.0** 仍能构建。功能上 HTTP/gRPC 等价,
  HTTP 还少一条依赖链。

## Consequences

- **Positive** —— 全机队一套指标名 + 一块看板;打开 OTel 即得跨服务调用链且
  日志自动带 trace_id;dev 默认零开销、零外部依赖;生产成本由采样比率封顶;
  压测脚本 + runbook 给出可复算的容量基线。

- **Negative / 已接受的风险** —— Python 与 Go 各维护一份 RED 实现(语义对齐靠
  约定与测试,而非共享代码);OTel 打开后 Go 侧 `grpc` 被顶到 v1.65.0(otelgrpc
  的下限),但 `go` 指令未被迫升级;sandbox-manager 作为独立 go 1.25 模块,其 otel
  间接依赖自由解析到较新版本,与 workspace 模块的固定版本不一致(各自独立构建,
  互不影响)。

- **Followups** —— 观测栈稳定后沉淀告警规则;给 Python 侧补一套独立的
  py-common 测试 harness(目前 tracing 单测寄居在 agent-runtime 套件);跟踪
  上游 otel 何时与 go 1.23 工具链解耦,届时统一各模块版本。
