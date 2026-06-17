# 变更记录：#15 Plan 增补 —— 吸收 Agent Substrate 的 runsc checkpoint/restore 路线

- 关联 Plan：`docs/plan/hardening-gvisor-spike-and-image-warmer.md`(§4.2 / §5 / §6)
- 关联任务：#15;新增 followup #17(中期评估 SubstrateProvider)
- 关联外部项目：Agent Substrate(github.com/agent-substrate/substrate)
- 日期：2026-06-17

## 背景

调研 Agent Substrate(Google 非官方开源,K8s 之上的 agent 多路复用系统)后确认:
其能力与 cocola 同问题域,且 runsc checkpoint/restore 实现「连易失 RAM 一起保留」
的亚秒级 suspend/resume,正好挑战 cocola ADR-0008「hibernate 删 Pod、RAM 必丢、
靠 claude --resume 重放」的既定假设。但该项目自述 v0.0.0「VERY early、不可生产、
API 必变」,且自带整套控制面 + CRD + Envoy,现阶段不整体并入。决策:短期仅吸收其
技术路线,中期再评估作为可插拔后端。

## 改动

- `docs/plan/hardening-gvisor-spike-and-image-warmer.md`:
  - §4.2 新增**探针 6**:runsc checkpoint/restore——在 runsc Pod 内对 Route-A
    进程做 `runsc checkpoint`/`restore`,验证能否 RAM-kept resume;含验收要点与
    通过/不通过的后续动作(通过则回写 ADR-0008 §3 的 RAM-lost 结论)。
  - §5 新增注:Agent Substrate 整体集成评估的边界(短期吸收路线、中期封
    SubstrateProvider、不整体并入),指向 followup task。
  - §6 新增风险条目:checkpoint/restore 对 Node 重 runtime 不保证可行,仅 Layer C
    可实测,不通过不阻塞其余验收。

## 范围说明

纯 Plan 文档增补,无代码/部署物改动。中期「评估 Agent Substrate 封装为可插拔
SubstrateProvider」另立 task 跟踪(待上游成熟度),不属 #15 范围。
