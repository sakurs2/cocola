# feat(m8 S1):新增 go-common/metrics 基座(Prometheus)

## 背景

M8 第一步(Plan §5 S1):为三个 Go 服务提供统一的 Prometheus instrumentation
基座,使 metric 命名、/metrics 端点、RED 中间件全队一致。复用
prometheus/client_golang,不造轮子。

## 改动

新增包 `packages/go-common/metrics/`:

- `metrics.go`:`Registry` 封装 per-service `prometheus.Registry`(非全局默认
  registry,保证测试隔离),默认注册 Go runtime + process collector;统一 RED
  向量 `cocola_requests_total`(transport/method/code)、
  `cocola_request_duration_seconds`(直方图,buckets 覆盖亚毫秒 RPC 到秒级冷启)、
  `cocola_requests_in_flight`;`service` 作为 const label 区分服务;
  `Registerer()/MustRegister()` 暴露 seam 供服务自定义 collector 挂载
  (sandbox-manager 的 orchestrator 桥接将用此 seam)。
- `http.go`:`HTTPMiddleware(route, next)` 记录 RED(transport=http),route 用
  调用方给定的稳定标签避免 path 参数导致的高基数;statusWriter 捕获状态码并实现
  Flusher 以不缓冲 SSE 流。
- `grpc.go`:`UnaryServerInterceptor()/StreamServerInterceptor()` 记录 RED
  (transport=grpc),method=FullMethod(有界),code=gRPC status string。
- `server.go`:`Mux()` 返回挂载 /metrics + /healthz 的 ServeMux。
  **遵守 network_security**:ListenAndServe 由各服务 main 在真实部署里调用,
  本包从不绑定端口;单测用 httptest。
- `metrics_test.go` / `dummy_test.go`:httptest 验 /metrics、service const label、
  HTTP/gRPC 中间件记录 RED(含状态码 418 / gRPC OK+NotFound)、自定义 collector
  经 MustRegister 挂载后出现在 exposition。

依赖与版本(为不裹挟下游做最小化收敛):

- go-common 引入 `prometheus/client_golang v1.20.5`(go1.20 兼容版,避免最新版
  顶高 go 指令)与 `google.golang.org/grpc v1.62.1`(对齐 gateway 已用版本)。
- 传递依赖 prometheus/common 钉 v0.55.0、x/net v0.26.0 等,避免被顶到要求
  go1.25 的版本。最终各 Go module 的 go 指令由 1.22 抬到 **1.23.0**(client_model
  v0.6.2 / protobuf 的下限,良性小版本),go.work 同步到 1.23.0。移除 toolchain
  指令以免钉死贡献者本地 toolchain。

## 验证

- `gofmt -l metrics/` 干净;`go vet ./metrics/...` 通过。
- `go test ./metrics/...` 全过(httptest,无真实端口)。
- go-common / gateway / admin-api 三 module `go build` + 既有 `go test` 全绿,
  零回归(workspace 模式 + GOWORK=off 单 module 模式均验证)。

## 后续

- S2:三个 Go 服务起 observability HTTP 端口并挂中间件;sandbox-manager 加
  orchestrator.Metrics 的 Prometheus collector 桥接。
- S3:两个 Python 服务接 prometheus-fastapi-instrumentator + gRPC interceptor。
