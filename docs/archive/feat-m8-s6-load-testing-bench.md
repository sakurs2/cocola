# feat(m8 S6):压测套件(k6 SSE + ghz gRPC)+ 容量基线 runbook

## 背景

M8 第六步(Plan §5 S6):在 S1–S5 把指标/链路埋好并能看板化之后,补上**主动施压**
的一环——产生可控负载,驱动 RED 看板,据此定单实例容量基线。覆盖两条关键路径:
用户会话的 HTTP/SSE 流(gateway `POST /v1/chat`)与运行时的 server-streaming gRPC
(agent-runtime `AgentRuntimeService/Query`)。设计与取舍见 ADR-0011。

核心约束:

- **复用开源**:k6(HTTP/SSE 负载事实标准)+ ghz(gRPC 对应物),不自研压测框架。
- **不监听端口**(`<network_security>`):脚本是**客户端**,连到使用者自行拉起的
  服务;本步不在沙箱内起任何监听进程。
- **CI 友好**:k6 内置 threshold 即流水线卡口(不达标非零退出);两脚本都带烟测档。

## 设计取舍

- **SSE 用 k6 http + 读到流结束**:k6 无原生 SSE 客户端,故以 `responseType:"text"`
  整流读完(agent 发完 `done`/`error` 即关流),用 `http_req_waiting` 近似 TTFB
  (gateway 先 flush 200 头再流),并解析 `event:` 帧计数事件、识别 in-band error。
- **gRPC 用 ghz 直吃 .proto**:`--proto packages/proto/...agent.proto --import-paths`,
  不依赖服务端反射,任何构建可压。Query 是 server-stream,ghz 保持流到服务端关闭,
  量到的是整条流式调用时延。
- **端口对齐 compose**:gateway `localhost:8080`、agent-runtime `localhost:50061`
  (compose 均已发布到宿主机);默认值与 `docker-compose.full.yml` 一致,零参可跑。
- **EchoProvider 基线 = 框架开销上限**:全栈默认 auth OFF + EchoProvider,测得的是
  框架本身天花板(无真实 LLM/沙箱延迟);接真实 provider 后另立基线行。
- **容量基线留模板而非假数**:runbook 给出"单服务隔离 → 全链路 → 定额定容量"的
  方法与回填表,数字在目标硬件上实测后随 commit 留痕,不杜撰。

## 改动清单

- `bench/k6/gateway_sse.js`(新增):ramping-vus 压 `POST /v1/chat`;自定义 metric
  `sse_ttfb_ms`/`sse_stream_ms`/`sse_events_total`/`sse_error_rate`;threshold
  `http_req_failed<1%`、`sse_error_rate<1%`、`sse_ttfb_ms p95<2s`;env 旋钮
  BASE_URL/TOKEN/VUS/DURATION/PROMPT/MAX_TURNS;含 1VU/5s 烟测档。
- `bench/ghz/agent_query.sh`(新增,可执行):ghz 压 `AgentRuntimeService/Query`,
  直引 `packages/proto`;env 旋钮 TARGET/CONC/TOTAL/DURATION/PROMPT/INSECURE;
  支持次数模式与时长模式;含 CONC=2/TOTAL=20 烟测档;缺 ghz 时友好报错退出 127。
- `bench/README.md`(新增):压测 runbook —— 前置(起全栈 + 观测栈 + 装 k6/ghz)、
  两路径用法、关注指标、**容量基线回填表(模板)**与定容量步骤、网络约束。

## 验证

- `node --check bench/k6/gateway_sse.js` 通过;`bash -n bench/ghz/agent_query.sh` 通过。
- 端口/端点核对自 `apps/gateway/internal/httpapi/api.go`(`POST /v1/chat`)、
  `packages/proto/cocola/agent/v1/agent.proto`(`AgentRuntimeService/Query`)与
  `docker-compose.full.yml`(8080 / 50061 发布)。
- 未在沙箱内启动任何进程(遵守 `<network_security>`)。

## 后续

- 在目标硬件上跑出首版基线数据并回填 `bench/README.md` 容量表。
- 接真实 LLM provider / K8s 沙箱后补一组"真实路径"基线。
- 至此 M8 六步(S1 指标基座 → S2/S3 五服务接入 → S4 链路 → S5 部署栈 → S6 压测)
  全部落地。
