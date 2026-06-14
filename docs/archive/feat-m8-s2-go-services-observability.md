# feat(m8 S2):三个 Go 服务接入可观测性

## 背景

M8 第二步(Plan §5 S2):把 S1 的 `go-common/metrics` 基座接到三个 Go 服务,
让 gateway / sandbox-manager / admin-api 都能被 Prometheus 抓取。每个服务在
独立的 observability 端口暴露 `/metrics` 与 `/healthz`,与业务端口隔离,抓取
不与用户流量争抢。sandbox-manager 额外把已有的、零依赖的
`orchestrator.Metrics` 内存 sink 桥接进 Prometheus,**不重写**原 sink。

## 改动

### go-common/metrics(基座小增量)

- `http.go`:新增 `HTTPHandler(routeFn func(*http.Request) string, next)`,
  在请求处理**之后**取 route 标签。这样 chi / stdlib mux 可以上报“匹配到的
  路由模板”(只有路由后才知道),既得到稳定标签又避免 path 参数撑爆基数。
  原 `HTTPMiddleware(route, next)` 改为它的固定标签特例。labeler 返回空串时
  归入 `unmatched` 桶。

### gateway(HTTP / SSE)

- `internal/httpapi/api.go`:`API` 增加可选 `metrics *metrics.Registry` +
  `WithMetrics()`;`Handler()` 用 `instrument()` 给 `/healthz`、`/v1/chat`
  套 RED 中间件(标签为固定路由模板,如 `POST /v1/chat`)。nil 时不插桩,
  单测保持轻依赖。中间件包住 auth + handler,延迟含鉴权。
- `cmd/gateway/main.go`:构造 `metrics.New("gateway")`,注入 API;
  `COCOLA_METRICS_ADDR`(默认 `:9091`,空则关闭)起独立端口挂 `reg.Mux()`。

### admin-api(HTTP / chi)

- `internal/httpapi/api.go`:同样加 `metrics` 字段 + `WithMetrics()`;在
  `Router()` 顶部用 chi 中间件 + `HTTPHandler`,标签取
  `req.Method + " " + chi.RouteContext().RoutePattern()`,即 `DELETE
  /admin/tokens/{id}` 这类模板,path 里的 id 不进标签。
- `cmd/admin-api/main.go`:`metrics.New("admin-api")` + `COCOLA_METRICS_ADDR`
  (默认 `:9093`)独立端口。

### sandbox-manager(gRPC + binder 桥接)

- `internal/obs/collector.go`(新):`BinderCollector` 实现
  `prometheus.Collector`,通过窄接口 `snapshotter` 读
  `orchestrator.Metrics.Snapshot()`,**每次抓取**惰性计算,emit:
  `cocola_sandbox_pool_hit_rate` / `_pool_hits_total` / `_pool_misses_total`
  / `_active_count` / `_create_p50_milliseconds` / `_create_p99_milliseconds`。
  无后台 goroutine、无状态复制,snapshot 是唯一真相源。
- `cmd/sandbox-manager/main.go`:`metrics.New("sandbox-manager")` 提前构造;
  gRPC server 挂 `Unary/StreamServerInterceptor`;Redis 可用分支里把
  `BinderCollector` 注册到 registry;`COCOLA_METRICS_ADDR`(默认 `:9092`)
  独立端口。

## 单测

- `gateway/internal/httpapi/api_test.go`:`TestMetricsInstrumentation` —— 经
  httptest 抓 `/metrics`,断言 `service="gateway-test"`、`transport="http"`、
  `method="POST /v1/chat"`、`code="200"` 均在。
- `admin-api/internal/httpapi/metrics_test.go`(新):`TestMetricsRoutePattern`
  —— 两个不同 token id 的 DELETE 都归到 `DELETE /admin/tokens/{id}` 模板,
  且断言原始 id **未**泄漏进标签(基数防护)。
- `sandbox-manager/internal/obs/collector_test.go`(新):用 fake snapshotter
  以 `testutil.GatherAndCompare` 精确比对四个指标输出;再用真实 sink 验证
  惰性读取 + 单序列。

## 依赖

- gateway / admin-api 经 go-common 间接引入 `prometheus/client_golang
  v1.20.5`、`grpc v1.62.1`、`prometheus/common v0.55.0` 等(均为 S1 已锁版本,
  零额外漂移)。
- sandbox-manager(独立 module,go 1.25)`go mod tidy` 直接引入
  `client_golang v1.20.5`,与 fleet 其余 module 同版本。

## network_security

- metrics 包从不绑定端口;`ListenAndServe` 只在各服务 `main` 中、真实部署里
  执行。单测一律走 httptest,不占真实端口。

## 验证

- 四个 module 全部 `go build` / `go test` 绿;`gofmt -l` 干净;`go vet` 通过。
- gateway / admin-api / sandbox-manager / go-common 既有用例零回归。
