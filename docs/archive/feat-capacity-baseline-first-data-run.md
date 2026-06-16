# 变更记录：容量基线首版数据 run(回填 bench runbook)

- 关联：M8 S6(`bench/`)、ADR-0011、Plan `docs/plan/m8-observability-and-load-testing.md`
- 支撑:#13 warm pool 决策
- 日期：2026-06-16

## 背景

M8 S6 已落地压测脚本(k6 SSE + ghz gRPC)与容量基线 runbook **模板**,但
`bench/README.md` §3 一直是待回填占位。本次在本机真实跑出首版基线数据并回填,
为后续 warm pool / 扩容决策提供数据支撑。

## 执行环境

- Docker Desktop VM `linux/aarch64`,12 vCPU / 8 GiB。
- 全栈 `docker-compose.full.yml`(auth OFF + EchoProvider + Docker sandbox
  provider)+ 观测栈 `docker-compose.observability.yml`。
- 本机 brew 源故障(formulae API 404),改用官方容器 `grafana/k6` /
  `ghcr.io/bojand/ghz` 接入 `cocola_default` 网络以服务名压测(更干净,免装二进制)。

## 数据结论

- **稳态吞吐(20 并发,EchoProvider)**:gateway SSE(k6)RPS 3.56、P50 5.12s、
  P95 6.13s、0 错误;agent-runtime Query(ghz)RPS 5.57、P50 3.47s、P95 4.68s、
  P99 5.08s、0 错误。两路径 20 并发零错误。
- **沙箱冷启 vs 复用(warm pool 关键输入)**:复用同 session 稳态 ~0.64s;每次新
  session 冷启 ~1.08s;**冷启净增量 ≈ 0.44s/请求**(本机 Docker provider)。
  warm pool 预热可把这部分从新会话首响应延迟中抹掉。
- **资源行为**:沙箱创建后转 Paused(每个 ~3.4 MiB),空闲后被 GC(压测产生的
  ~20 个沙箱数分钟内归零),保活成本低、回收有效。

## 改动

- `bench/README.md` §3:由"待回填模板"改为"首版基线",填入 3.1 稳态吞吐表、
  3.2 冷启/复用对照表、3.3 复现步骤(含容器化 k6/ghz 命令)、3.4 已知缺口。

## 已知缺口(随基线留痕,留待后续)

- Prometheus 跨网抓取:服务 metrics 端口(9091–9094)在 Prometheus 里 `up=0`,
  scrape 目标端口/网络待校准;本次以 k6/ghz 自带统计为权威数据源,Grafana RED
  端到端联通留作 S5 收尾项单独修。
- 真实 LLM provider 路径基线、K8s + gVisor 冷启 p99:待目标集群复测(与 #15 同批),
  用于校准 warm pool 容量与预热个数。

## 收尾

压测后已 `docker compose down` 全栈 + 观测栈,清理残留沙箱容器。未在沙箱内启动任何
监听进程(k6/ghz 为客户端容器,连到自起的服务)。
