# feat(m8 S5):部署观测栈(Prometheus + Grafana + Tempo + 可选 OTel Collector)

## 背景

M8 第五步(Plan §5 S5):把 S1–S4 在五个服务里埋好的**指标(RED)**与**链路
(OTel)**真正"接出来"——提供一键拉起的观测后端,以及 K8s 侧的 Helm 开关。让
开发者在本地 `docker compose up` 即可看到机队级 RED 仪表盘,并在打开 trace 开关后
于 Grafana 里按 trace 钻取。设计与取舍见 ADR-0011。

核心约束:

- **复用开源**:Prometheus + Grafana + Tempo + OpenTelemetry Collector(contrib),
  全部官方镜像 + 声明式 provisioning,不自研采集/存储/看板。
- **不监听端口**(`<network_security>`):本步只产出配置与 compose/Helm 清单,
  不在沙箱内启动任何进程。
- **side-car 叠加**:观测栈作为独立 compose 贴到主栈网络上,主栈零改动即可被采集。
- **默认零摩擦**:指标默认开;链路默认关(与 ADR-0011 一致),打开只需注入 env。

## 设计取舍

- **Prometheus 单 job/服务 + 由指标自带 `service` 标签聚合**:五服务共用同一 RED
  契约且每条指标自带 `service` 标签,Prometheus 只需知道"去哪抓",看板按 `service`
  分组即可,无需 relabel 造标签。
- **观测栈用独立 compose + external network 贴主栈**:`docker-compose.observability.yml`
  以 `name: cocola-observability` 自成 project,通过 `networks.cocola: {external: true,
  name: cocola_default}` 加入主栈(`docker-compose.full.yml`,project `cocola`)网络,
  从而用 compose DNS 名(`gateway:9091` 等)直接抓取。容器间走 docker 网络任意端口
  互通,服务**无需**把 metrics 端口发布到宿主机。
- **Tempo 单体(monolithic)+ 本地盘**:本地开发用单进程 Tempo,OTLP/HTTP(4318)
  与 gRPC(4317)双收,块本地存储、24h 保留;生产换分布式部署即可,契约不变。
- **OTel Collector 设为可选(compose profile `collector`)**:默认服务直推 Tempo,
  零中间件;需要批处理/尾采样/多后端扇出时再 `--profile collector` 拉起,把服务
  指到 `otel-collector:4318`。
- **Grafana 全声明式 provisioning**:datasource(Prometheus 默认 + Tempo)、
  dashboard provider、RED 看板 JSON 全部随镜像挂载,开箱即用、可版本化;匿名
  Viewer 打开,admin/admin 便于本地。
- **Helm 侧 observability 开关**:`values.observability.{metrics,tracing}` 两段开关。
  metrics 默认开(暴露 9092 端口 + 可选 ServiceMonitor);tracing 默认关(注入
  `COCOLA_OTEL_*` env)。ServiceMonitor 由 `metrics.serviceMonitor` 单独门控,
  无 Prometheus-Operator CRD 的集群渲染干净清单。

## 改动清单

### 观测后端配置(`deploy/observability/`)

- `prometheus/prometheus.yml`:全局 15s 抓取,`external_labels.cluster=cocola-local`;
  6 个 job —— gateway:9091 / sandbox-manager:9092 / admin-api:9093 /
  agent-runtime:9094 / llm-gateway(`/metrics` @ :8080) / prometheus 自身。
- `tempo/tempo.yaml`:单体 Tempo,OTLP http:4318 + grpc:4317 receiver,本地盘
  (`/var/tempo/wal`、`/var/tempo/blocks`),块保留 24h,HTTP API :3200。
- `otel-collector/config.yaml`:可选扇入 collector,OTLP 双收 + batch/memory_limiter
  处理器 + `otlp/tempo` 导出(tempo:4317 insecure)。
- `grafana/provisioning/datasources/datasources.yaml`:Prometheus(默认)+ Tempo。
- `grafana/provisioning/dashboards/dashboards.yaml`:file provider 加载
  `/var/lib/grafana/dashboards` 到文件夹 "cocola"。
- `grafana/dashboards/cocola-red.json`:"cocola — Fleet RED" 看板(uid=cocola-red),
  模板变量 `$service`/`$transport`,6 面板 —— 请求速率、错误率(code!~"2..|OK")、
  P99/P50 时延、in-flight、Top methods。

### 一键拉起(`deploy/docker-compose/`)

- `docker-compose.observability.yml`(新增):Prometheus(:9090)、Grafana(:3001→3000)、
  Tempo(:3200/:4318/:4317),可选 otel-collector(profile `collector`,host :14318/:14317)。
  挂载上述配置;`cocola` 网络声明为 external 贴主栈。

### Helm(`deploy/helm/cocola-sandbox/`)

- `values.yaml`(改):新增 `observability.metrics`(enabled/port/serviceMonitor/interval)
  与 `observability.tracing`(enabled/endpoint/insecure/samplerRatio)两段开关。
- `templates/sandbox-manager.yaml`(改):metrics 开时暴露 `metrics` containerPort +
  注入 `COCOLA_METRICS_ADDR`,并在 Service 暴露 metrics 端口;tracing 开时注入
  `COCOLA_OTEL_*` env。
- `templates/servicemonitor.yaml`(新增):门控于 metrics.enabled && metrics.serviceMonitor
  的 Prometheus-Operator ServiceMonitor。

## 验证

- 全部 YAML 经 PyYAML `safe_load_all` 解析通过;`cocola-red.json` 经 `json.load` 通过。
- Helm 模板按 `if .Values.observability.*` 正确门控(grep 校验渲染分支)。
- 未在沙箱内启动任何进程(遵守 `<network_security>`);compose/Helm 仅为声明式清单。

## 后续

- S6:压测脚本(k6 SSE + ghz gRPC)+ 容量基线 runbook。
