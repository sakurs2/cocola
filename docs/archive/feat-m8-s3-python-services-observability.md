# feat(m8 S3):两个 Python 服务接入可观测性

## 背景

M8 第三步(Plan §5 S3):把 S1/S2 在 Go 侧确立的 RED 指标契约镜像到 Python
服务,让 llm-gateway / agent-runtime 也能被同一套 Prometheus + Grafana 看板
覆盖。核心约束是**指标名与标签必须与 Go 端逐字一致**,polyglot 车队才能共用
一张仪表盘:

- `cocola_requests_total{service,transport,method,code}`
- `cocola_request_duration_seconds{service,transport,method}`
- `cocola_requests_in_flight{service,transport}`

`service` 为常量标签区分车队;`transport` 取 `http`/`grpc`。直方图 bucket 与
go-common 完全相同(亚毫秒 RPC 到多秒沙箱冷启动)。

## 设计取舍

- **复用 `prometheus_client`**(规范库),适配层很薄(一个路由感知的 ASGI
  中间件 + 一个 grpc.aio 拦截器)。**刻意不用 `prometheus-fastapi-
  instrumentator`**:它自带一套指标名,会让看板与 Go 服务分叉;手写约 40 行
  中间件即可对齐 Go 的 RED 契约,值得。
- **每服务一个 `CollectorRegistry`**(而非进程级 default),单测因此 hermetic,
  两个 registry 不会串。
- **SSE 安全**:用纯 ASGI 中间件,而非 Starlette `BaseHTTPMiddleware`(后者会
  缓冲响应体,破坏 llm-gateway 的 SSE 流)。中间件只观测 response-start 状态码
  与墙钟耗时,从不碰 body,流式原样 flush。
- **基数防护**:HTTP `method` 标签取**路由后**的模板 `"<METHOD> <route
  template>"`(`scope["route"].path`),路径参数不进标签;未匹配落入
  `unmatched`。gRPC `method` 取 full method 名(有界集合)。
- 按 `<network_security>`:metrics 模块**从不绑定端口**。llm-gateway 把
  `/metrics` 挂在既有 FastAPI app 上(不额外开端口);agent-runtime 无 HTTP
  server,仅在真实部署的 `serve()` 里用 `start_http_server` 暴露,单测从不起监听。

## 改动

### py-common(共享基座)

- `cocola_common/metrics.py`(新):`Registry`(per-service registry + 三个 RED
  向量 + `ProcessCollector`/`PlatformCollector`/`GCCollector` 免费基线)、
  `observe_request`/`inflight_inc`/`inflight_dec` 录制缝、`render()` 导出;
  `PrometheusASGIMiddleware`(纯 ASGI、SSE 安全);`instrument_fastapi(app,
  registry)` 用**普通 route**(非 `app.mount`)挂 `GET /metrics`,避免末尾
  斜杠的 307 跳转(Prometheus 不跟随重定向)。
- `cocola_common/metrics_grpc.py`(新):`PrometheusServerInterceptor`
  (grpc.aio)。单独成模块,使 `import cocola_common.metrics` **不需要** grpc
  (llm-gateway 无 grpc 依赖)。`intercept_service` 按 unary_unary /
  unary_stream 两种 server 端 arity 重建 handler,保留 (de)serializer;
  `code` 取 StatusCode 名(`None`→`OK`,未捕获异常→`UNKNOWN`),对齐 Go 端
  `status.Code(err).String()`。duration 覆盖整个 handler——对 server-streaming
  的 Query 即"整轮 agent 耗时"。
- `cocola_common/__init__.py`:导出 `Registry`、`instrument_fastapi`。
- `pyproject.toml`:dependencies 加 `prometheus-client>=0.20`。

### llm-gateway(HTTP / SSE)

- `server.py`:`create_app` 加可选 `metrics: Registry | None` kwarg;非空时
  `instrument_fastapi(app, metrics)`。nil 时不插桩,单测保持轻依赖。
- `__main__.py`:构造 `Registry("llm-gateway")` 注入 `create_app`,`/metrics`
  与业务同 app,不额外开端口。

### agent-runtime(gRPC)

- `__main__.py`:`Registry("agent-runtime")` + `grpc.aio.server(interceptors=
  [PrometheusServerInterceptor(metrics)])`;`COCOLA_METRICS_PORT`(默认 9094,
  空/0 则关闭)经 `start_http_server` 暴露独立 `/metrics`——agent-runtime 无
  HTTP server,故走 prometheus_client WSGI 端口。

## 单测

- `apps/llm-gateway/tests/test_metrics.py`(新):POST /v1/messages 后抓
  /metrics,断言 `service`/`transport`/`method`/`code` 标签齐全且含
  `python_info`;另验 SSE 仍完整 emit `message_start…message_stop` 且只计费
  一次(中间件不破坏流式)。
- `apps/agent-runtime/tests/test_metrics.py`(新):**hermetic**——按
  `<network_security>` 不起 socket,直接驱动 `intercept_service` 并调用被包裹
  的 behavior。断言 unary-stream 成功路径 `code="OK"`、流式输出透传、
  in-flight 归零、duration 计数为 1;未捕获异常路径 `code="UNKNOWN"`;以及
  unary_unary arity 透传仍正确录制。

## 验证

- llm-gateway:`103 passed, 3 skipped`(忽略预存在的 `test_token_passthrough_
  e2e.py`——该跨 app e2e 需 proto 的 PYTHONPATH,与本次改动无关,改动前后同样
  失败)。
- agent-runtime:`51 passed, 2 skipped`,含新增 3 条拦截器用例。
- 三处 `.venv` 经 `uv sync` 装入 `prometheus-client 0.25.0`;`ruff format` +
  `ruff check` 全绿。
- `python_info`(PlatformCollector)为跨平台基线:`ProcessCollector` 在非
  Linux(macOS 无 /proc)为 no-op,故断言 `python_info` 而非
  `process_cpu_seconds_total`。
