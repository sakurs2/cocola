# docs(m8):落地 M8 可观测性与压测 Plan

## 背景

M0–M7 功能链路已通,但几乎不可观测:唯一 metrics 是 sandbox-manager 内一个
进程内零依赖内存 sink(无 HTTP 端点、重启即丢、跨副本不可聚合),五个服务均无
/metrics 端点,日志无 trace_id 贯穿,且无任何压测基线。M8 目标:让 cocola
可观测(metrics+日志+tracing)且有压测基线与容量结论,为 M6+ warm pool 决策
提供数据支撑。

## 改动

- 新增 `docs/plan/m8-observability-and-load-testing.md`(143 行,待评审):
  - 范围:metrics(go-common/metrics 基于 client_golang + Python
    prometheus-fastapi-instrumentator)、结构化日志+trace 关联、OTel 链路追踪、
    部署物(Prometheus/Grafana/可选 OTel Collector+Tempo/Jaeger)、压测
    (k6 压 SSE、ghz 压 gRPC)。
  - 开源选型表:全部 CNCF/Grafana 生态主流开源,自托管零授权成本。
  - 关键设计:不破坏现有 orchestrator.Metrics(加 collector 适配层读 Snapshot,
    不改写),/metrics 走独立 HTTP 端口(遵守 network_security:本地沙箱不起
    监听端口,仅真实部署容器/集群跑;单测用 httptest),不开观测栈零额外延迟。
  - 实施分 6 步(S1 go-common/metrics 基座 → S2 Go 接入 → S3 Py 接入 →
    S4 日志 trace+OTel → S5 部署物 → S6 压测),每步配单测与 changelog。

## 验证

- 仅新增 Plan 文档与本 changelog,无代码/部署改动。
- 文件无 tab,prettier 友好。

## 后续

- Plan 经用户确认后按 S1–S6 实现;建议先交付 S1–S3(metrics)验证价值,
  再推进 S4–S6。
- 实现阶段将新增 ADR-0011 记录三支柱选型与开关策略。
