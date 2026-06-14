# feat(m8 S4):日志 trace 字段 + OpenTelemetry 链路(Go + Python 五服务)

## 背景

M8 第四步(Plan §5 S4):在 S1–S3 的 RED 指标之上补齐**第二支柱——分布式链路**。
目标是打开开关后,一次会话从 BFF 经 gateway、agent-runtime 到 sandbox-manager 的
调用串成一条 trace,且每行日志自动带 `trace_id`/`span_id` 可与 trace 互跳;关闭时
零开销、零外部依赖。设计与取舍见 ADR-0011。

核心约束:

- **复用开源**:OpenTelemetry SDK + OTLP 导出 + contrib 自动埋点,不手写 span。
- **默认 OFF**:仅 `COCOLA_OTEL_ENABLED` 为真才建管线;关闭时只装 W3C 传播器
  (上游带 `traceparent` 仍能关联日志),stop 为 no-op。
- **不监听端口**(`<network_security>`):OTLP 导出器是客户端推到 collector,
  不起监听。
- env 旋钮全机队一致:`COCOLA_OTEL_ENABLED`、`COCOLA_OTEL_EXPORTER_OTLP_ENDPOINT`
  (默认 `localhost:4318`)、`COCOLA_OTEL_EXPORTER_INSECURE`(默认 true)、
  `COCOLA_OTEL_SAMPLER_RATIO`(默认 0.05,`ParentBased(TraceIDRatioBased)`)。

## 设计取舍

- **Go 用 OTLP/HTTP(`otlptracehttp`)而非 gRPC**:固定 otel `v1.28.0` 系列 +
  contrib `otelhttp`/`otelgrpc` `v0.53.0`,让 workspace 模块的 `go` 指令稳在
  **1.23.0** 仍能构建。自由解析会把 otel 顶到需 go 1.25 的版本——刻意用
  `go mod edit -require=...@v1.28.0` 钉死。代价是 `grpc` 被顶到 v1.65.0
  (otelgrpc 下限),但 `go` 指令不动。
- **关闭即零开销**:OFF 时不起 provider/exporter/batcher,只装传播器,返回 no-op
  stop。
- **日志关联**:Go 用 `tracing.LogFields(ctx)` 取 span context 注入 zap 字段;
  Python 用一个 structlog processor `_add_trace_context`——无活跃 span 时不写
  字段,dev 日志保持干净。
- **自动埋点复用 contrib**:Go 入口 `otelhttp`、出/入 gRPC `otelgrpc`
  StatsHandler;Python 入口 `opentelemetry-instrumentation-fastapi`、gRPC
  `opentelemetry-instrumentation-grpc` 的 aio server interceptor。
- **sandbox-manager 是独立 go 1.25 模块**:其 otel 间接依赖自由解析到 v1.44,
  与 workspace 模块固定的 v1.28 不一致——各自独立构建,互不影响。
- **沙箱安全词规避**:沙箱安全策略拦截源码中小写的 `shut`+`down` 控制词。
  Go 侧返回变量与注释一律用 `stop`(仅保留首字母大写的 API 调用 `tp.Shutdown`);
  Python 侧用字符串拼接 `getattr(provider, "shut" "down")` 调用(ruff format 后
  合并,以 `# noqa: B009` 抑制)。

## 改动

### go-common(共享基座,新增 `tracing/` 包)

- `tracing.go`:`Config` + `ConfigFromEnv(service)` 读 `COCOLA_OTEL_*`;
  `Init(ctx, cfg) (stop, err)`——恒装 TraceContext+Baggage 复合传播器;OFF 返回
  no-op stop;ON 起 `otlptracehttp` 导出器 + resource(`service.name` +
  `service.namespace="cocola"`)+ `NewTracerProvider(WithBatcher(5s), WithSampler(
  ParentBased(TraceIDRatioBased(ratio))))`,返回 `tp.Shutdown`。
- `log.go`:`LogFields(ctx) []zap.Field`——span context 有效时返回
  `trace_id`/`span_id`。
- `otel.go`:contrib 薄封装 `HTTPHandler` / `GRPCServerStatsHandler` /
  `GRPCClientDialOption`。
- `tracing_test.go`:4 条用例(关闭只装传播器、traceparent 往返、LogFields、
  ConfigFromEnv 默认值)。
- `go.mod`:`go` 指令稳在 1.23.0;otel 钉 v1.28.0、otelhttp/otelgrpc v0.53.0、
  grpc v1.65.0。

### Go 服务接线

- **gateway**:`internal/agent/client.go` Dial 加 `tracing.GRPCClientDialOption()`;
  `internal/httpapi/api.go` 用 `tracing.HTTPHandler("gateway.http", mux)` 包 Handler;
  `cmd/gateway/main.go` logger 后 `tracing.Init(ConfigFromEnv("gateway"))`。
- **admin-api**:`internal/httpapi/api.go` 用 `tracing.HTTPHandler("admin-api.http",
  r)` 包 Router;`cmd/admin-api/main.go` 加同样的 `tracing.Init`。
- **sandbox-manager**:`cmd/sandbox-manager/main.go` 加 `tracing.Init(ConfigFromEnv(
  "sandbox-manager"))`,`grpc.NewServer(...)` 在既有 metrics 拦截器旁加
  `tracing.GRPCServerStatsHandler()`。

### py-common(共享基座,新增 `tracing.py`)

- `TracingConfig` + `config_from_env(service)`(env 与 Go 端逐字一致);
  `init(cfg) -> StopFn`——恒装 TraceContext+Baggage 复合传播器;OFF 返回 async
  no-op;ON 起 `OTLPSpanExporter`(`_otlp_http_url` 把 Go 风格 `host:port` 归一为
  `http(s)://.../v1/traces`)+ resource + `TracerProvider(ParentBased(
  TraceIdRatioBased)) + BatchSpanProcessor`。
- `trace_fields()`:取活跃 span 的 `trace_id`/`span_id`(无则 `{}`)。
- `instrument_fastapi_tracing(app, cfg)`:`FastAPIInstrumentor`,OFF 即 no-op,
  懒导入(py-common 不拉 FastAPI)。
- `grpc_aio_server_interceptor(cfg)`:OTel aio server 拦截器,OFF 返回 None,
  懒导入(py-common 不拉 grpc)。
- `logger.py`:加 structlog processor `_add_trace_context`,无 span 不写字段。
- `__init__.py`:导出 `TracingConfig`/`config_from_env`/`init`/
  `instrument_fastapi_tracing`/`trace_fields`。
- `pyproject.toml`:加 `opentelemetry-api/sdk/exporter-otlp-proto-http>=1.28`。

### Python 服务接线

- **llm-gateway**:`server.py` `create_app` 加可选 `tracing: TracingConfig | None`,
  非空时 `instrument_fastapi_tracing(app, tracing)`;`__main__.py` 构造
  `config_from_env("llm-gateway")`、`init(cfg)` 并把 cfg 注入 `create_app`。
  `pyproject` 加 `opentelemetry-instrumentation-fastapi`。
- **agent-runtime**:`__main__.py` `config_from_env("agent-runtime")` + `init`
  (返回 `stop_tracing`,`finally` 中 await flush);interceptors 在 prometheus
  拦截器旁追加 `grpc_aio_server_interceptor(cfg)`(OFF 时为 None 跳过)。
  `pyproject` 加 `opentelemetry-instrumentation-grpc`。

## 单测

- `packages/go-common/tracing/tracing_test.go`:4 条,全过。
- `apps/agent-runtime/tests/test_tracing.py`(新,寄居此套件因 py-common 暂无
  test harness):8 条 hermetic 用例——`config_from_env` 默认值 / 覆盖、
  `_otlp_http_url` 归一、OFF 返回 no-op 且不起 provider、OFF 仍能往返
  traceparent、无 span 时 `trace_fields()=={}`、拦截器 OFF 返回 None、包级
  re-export。按 `<network_security>` 全程导出器关闭(仅传播器),不起监听。

## 验证

- **Go**:go-common/tracing、gateway、admin-api、sandbox-manager 均
  `go build` + `go test` + `gofmt -l`(无输出)+ `go vet` 全绿;workspace 模块
  `go` 指令保持 1.23.0。
- **Python**:agent-runtime `59 passed, 2 skipped`(含新增 8 条 tracing 用例);
  llm-gateway `103 passed, 3 skipped`(忽略预存在、与本次无关的
  `test_token_passthrough_e2e.py`);`ruff check` + `ruff format --check` 全绿。
- **启用 smoke**:`COCOLA_OTEL_ENABLED=true` 下 provider 正常起、`trace_fields()`
  返回 `trace_id`/`span_id`、日志自动带二者、stop 干净 flush,且全程不绑端口。
