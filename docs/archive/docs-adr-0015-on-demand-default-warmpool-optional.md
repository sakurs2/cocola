# Changelog: ADR-0015 默认按需分配、warm pool 降为可选

日期: 2026-06-28
关联: task #27、ADR-0015(新增)、ADR-0008 §3、ADR-0012、ADR-0014、ADR-0002

## 背景
ADR-0012 在 Docker+K8s 并存语境下,已把「预热整箱+领用后挂卷」(adopt-by-remount)
否决为 warm pool 主路径,改 K8s 走「DaemonSet 节点镜像预拉」。此后 ADR-0014 退役 k8s
provider、把 OpenSandbox 定为唯一主后端,使 0012 两个前提失效:① DaemonSet 预拉是 K8s
专属,无载体;② 实测 OpenSandbox 运行中沙箱 API(metadata/pause/resume/renew/snapshots/
proxy)无任何增删卷端点,volumes 仅创建时可指定,adopt-by-remount 在唯一主后端仍不可行。
故需在 OpenSandbox-only 语境下重新表态分配主路径。

## 改动
### docs/adr/0015-on-demand-allocation-default-warm-pool-optional.md(新增)
- 决策:确立「按需冷启动分配」为默认且唯一主路径;warm pool 降为默认关闭的可选高并发
  优化;Pool/tryAdopt/Adopter 缝口及单测原地保留不删;承认 warm pool 在当前主后端无收益;
  未来若需收益方向为「按 (user,session) 预测性预创建」;main.go 告警增强;回链 0008/0012。
- Alternatives:A 维持 0012 现状 / B 为 OpenSandbox 实现 Adopter / C 根挂越权 /
  D 删 warm pool / E 选定方案,逐条记录否决理由。

### docs/adr/0008-persistence-layering-and-vault.md
- §3 warm pool 段尾追加 ADR-0015 指向:OpenSandbox-only 下默认按需分配、warm pool 可选
  且当前无后端可 adopt。

### docs/adr/0012-warm-pool-prewarm-strategy-under-pvc-volume-model.md
- Status 行标注「由 ADR-0015 再次收敛」;文末新增 Amendment 段说明两前提随 0014 变化。

### apps/sandbox-manager/cmd/sandbox-manager/main.go
- warm-pool-enabled-but-no-Adopter 的启动 warn 文案增强:明确「pool 将空转、无收益,
  当前主后端建议保持关闭(COCOLA_WARMPOOL_ENABLED),见 ADR-0015」,并补代码注释。

### README.md
- 路线图 WP 行更新:标注 warm pool 为「默认关闭的可选优化」、当前主后端开启亦只空转。

## 验收
- `GOWORK=off go build ./...` 绿(main.go 改动编译通过)。
- 纯文档 + 一处日志文案,无行为变更;按需分配本就是默认路径(pool 默认关闭)。

## 不含
- 端到端 chat 验收(单列 task #28)。
- 任何 warm pool 代码回退或删除。
