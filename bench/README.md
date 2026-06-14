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

## 3. 容量基线(待回填)

> 下表为**模板**。在目标硬件上按"1.单服务隔离压 → 2.全链路压"两步跑出数据后回填,
> 并附 commit / 硬件 / 镜像 tag。EchoProvider 路径测的是**框架开销上限**(无真实
> LLM/沙箱延迟);接真实 provider 后另立一行。

| 日期 | 硬件 | 路径 | 工具 | 并发 | RPS | P50 | P99 | 错误率 | 备注 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| _待测_ | _e.g. M2/8c16g_ | gateway SSE | k6 | 20 | _?_ | _?_ | _?_ | _?_ | EchoProvider |
| _待测_ | 同上 | agent-runtime Query | ghz | 20 | _?_ | _?_ | _?_ | _?_ | EchoProvider |

回填步骤:

1. **单服务隔离**:只起被测服务 + 其直接依赖,排除上下游噪声,确定单点天花板。
2. **全链路**:起全栈,从 gateway 压,观察瓶颈服务(Grafana 里哪个 `service`
   先到 P99 拐点 / in-flight 堆积)。
3. **定容量**:取 P99 仍在阈值内的最大 RPS 为单实例额定容量,按 SLO 留 buffer。
4. 把数字 + 环境写回上表,随 commit 留痕。

## 约束

- 所有脚本**不在沙箱内启动监听进程**;它们是客户端,连到你自己起的服务。
- 阈值即 CI 卡口:k6 阈值不达标会非零退出,可直接进流水线做回归护栏。
