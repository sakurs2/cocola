# cocola 压测与容量基线(M8 S6)

本目录是 cocola 的负载测试套件,配合 M8 的可观测栈(`deploy/observability/`)
使用:压测产生流量 → Prometheus 采集 RED 指标 → Grafana "Fleet RED" 看板观察,
据此定容量基线。两条关键路径各一个工具:

| 路径 | 协议 | 被测端点 | 工具 | 脚本 |
| --- | --- | --- | --- | --- |
| 用户会话流式 | HTTP/SSE | gateway `POST /v1/chat` | [k6](https://k6.io) | `k6/gateway_sse.js` |
| 运行时调用 | gRPC(server-stream) | agent-runtime `AgentRuntimeService/Query` | [ghz](https://ghz.sh) | `ghz/agent_query.sh` |

> **复用开源,不造轮子**:k6 是 SSE/HTTP 负载事实标准,ghz 是 gRPC 的对应物,
> 二者都开源且可脚本化进 CI。我们只写"被测什么 + 阈值",不自研压测框架。

## 0. 前置

```bash
# 起全栈(auth OFF + EchoProvider,零配置即可压;见 compose 注释)
docker compose -f deploy/docker-compose/docker-compose.full.yml up -d
# 叠加观测栈(贴主栈网络)
docker compose -f deploy/docker-compose/docker-compose.observability.yml up -d

# 工具(macOS)
brew install k6 ghz
```

Grafana:<http://localhost:3001>(匿名 Viewer);看板 "cocola — Fleet RED"。
压测时把 `$service` 选到 `gateway` / `agent-runtime` 观察速率、错误率、P50/P99。

## 1. k6 — gateway SSE

```bash
# 烟测(1 VU / 5s,CI 用)
k6 run -e VUS=1 -e DURATION=5s bench/k6/gateway_sse.js

# 基线(20 VU / 30s 稳态)
k6 run -e BASE_URL=http://localhost:8080 -e VUS=20 -e DURATION=30s \
  bench/k6/gateway_sse.js

# 开了 auth 时带 token
k6 run -e TOKEN="$JWT" -e VUS=50 -e DURATION=60s bench/k6/gateway_sse.js
```

关注指标:`http_req_failed`(<1%)、`sse_error_rate`(<1%)、`sse_ttfb_ms`
(p95<2s,内置阈值,失败即非零退出,适合 CI 卡口)、`sse_stream_ms`(全流耗时)。

## 2. ghz — agent-runtime gRPC

```bash
# 烟测
CONC=2 TOTAL=20 bench/ghz/agent_query.sh

# 基线(并发 20,2000 次)
CONC=20 TOTAL=2000 bench/ghz/agent_query.sh localhost:50061

# 时长模式(并发 50 跑 30s)
CONC=50 DURATION=30s bench/ghz/agent_query.sh
```

ghz 直接吃 `packages/proto` 里的 `.proto`,无需服务端反射。输出含 RPS、
P50/P90/P99 及状态码分布。

## 3. 容量基线(首版,2026-06-16)

> EchoProvider 路径测的是**框架 + 沙箱编排开销**(LLM 调用被 echo 替掉,但沙箱
> 仍真实创建);接真实 provider 后另立一组"真实路径"基线行。

**环境**:Docker Desktop VM `linux/aarch64`,12 vCPU / 8 GiB;全栈
`docker-compose.full.yml`(auth OFF + EchoProvider,Docker sandbox provider);
k6/ghz 经官方容器(`grafana/k6`、`ghcr.io/bojand/ghz`)接入 `cocola_default`
网络,以服务名压测。压测 commit:见本次提交。

### 3.1 稳态吞吐(20 并发)

| 日期 | 硬件 | 路径 | 工具 | 并发 | 样本 | RPS | P50 | P95 | P99 | 错误率 | 备注 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-06-16 | aarch64 12c/8g | gateway SSE | k6 | 20 VU | 167 | 3.56 | 5.12s | 6.13s | — | 0% | EchoProvider,每迭代新 session |
| 2026-06-16 | 同上 | agent-runtime Query | ghz | 20 | 200 | 5.57 | 3.47s | 4.68s | 5.08s | 0% | EchoProvider,每请求新 session |

> 20 并发下两路径均 **0 错误**。延迟主要被「每请求新建沙箱(冷启)+ 并发排队」
> 拉高:k6 经 gateway→agent-runtime 多一跳且 SSE 整流读完,故 P50 高于 ghz 直压。
> 烟测(1 VU)下 SSE 全流 med ~0.95s、TTFB ~0.1ms(gateway 立即 flush 头),
> 印证瓶颈在下游沙箱编排而非网关。

### 3.2 沙箱冷启 vs 复用(warm pool 决策输入,单并发排队外)

同一 `session_id` 复用已有沙箱,新 `session_id` 触发创建。单并发逐请求计时:

| 路径 | 首请求(冷启,建沙箱) | 稳态(复用沙箱) | 冷启净增量 |
| --- | --- | --- | --- |
| 复用同 session ×6 | 1.69s(#1) | ~0.62–0.67s(#2–6) | — |
| 每次新 session ×6 | ~1.0–1.16s(均值 ~1.08s) | — | **≈ 0.44s / 请求** vs 复用 ~0.64s |

> **结论(喂给 #13 warm pool)**:Docker provider 下沙箱冷启给每个新会话**首请求**
> 叠加约 **0.4–1.0s**(视是否已有暖容器)。warm pool 预热若能让新会话直接领到
> 暖沙箱,可把这部分从用户首响应延迟中抹掉。注:本机为 Docker provider;K8s +
> gVisor 冷启更重(镜像 1.9GB + runsc 初始化),真实增量需在目标集群复测(#15)。
> 编排层观察到沙箱创建后转 **Paused**(每个仅 ~3.4 MiB),且空闲后被 GC 回收
> (压测产生的 ~20 个沙箱在数分钟内归零),说明保活成本低、回收有效。

### 3.3 复现步骤

1. 起栈:`docker compose -f deploy/docker-compose/docker-compose.full.yml up -d`
   (可选叠 `docker-compose.observability.yml`)。
2. 工具:本机无 k6/ghz 时用官方容器(见上),或 `brew install k6 ghz`。
3. k6:`docker run --rm --network cocola_default -v "$PWD/bench/k6:/scripts:ro"
   -e BASE_URL=http://gateway:8080 -e VUS=20 -e DURATION=30s grafana/k6 run
   /scripts/gateway_sse.js`。
4. ghz:`docker run --rm --network cocola_default -v "$PWD/packages/proto:/proto:ro"
   ghcr.io/bojand/ghz --proto cocola/agent/v1/agent.proto --import-paths /proto
   --call cocola.agent.v1.AgentRuntimeService.Query
   -d '{"prompt":"ping","session_id":"ghz-{{.RequestNumber}}","max_turns":1}'
   -c 20 -n 200 --insecure agent-runtime:50061`。
5. 定容量:取 P99 仍在 SLO 内(默认 `sse_ttfb_ms p95<2s`、错误率<1%)的最大 RPS
   为单实例额定容量,按 SLO 留 buffer。

### 3.4 已知缺口 / 后续

- **Prometheus 跨网抓取**:观测栈起后,服务 metrics 端口(9091–9094)在
  Prometheus 里 `up=0`(scrape 目标端口/网络待校准);本次基线以 k6/ghz 自带
  统计为权威数据源,Grafana RED 看板的端到端联通留作 S5 收尾项单独修。
- **真实路径基线**:接真实 LLM provider 后补一组(含真实 token 时延)。
- **K8s + gVisor 基线**:冷启 p99 在目标集群复测(与 #15 同批),用于校准
  warm pool 容量与预热个数。复测脚本见 `deploy/k8s/verify-gvisor.sh`
  (探针 5,`RUN_COLDSTART=1`,跑「开/关节点镜像预拉」两组对照);跑通后把
  runsc 下的冷启数据连同预拉收益回填到上面 §3.2 表。

## 约束

- 所有脚本**不在沙箱内启动监听进程**;它们是客户端,连到你自己起的服务。
- 阈值即 CI 卡口:k6 阈值不达标会非零退出,可直接进流水线做回归护栏。
