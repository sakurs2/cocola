# 变更记录：#15 Plan —— gVisor 验收门 + K8s warm-pool 节点镜像预拉

- 关联 Plan：`docs/plan/hardening-gvisor-spike-and-image-warmer.md`
- 关联 ADR：ADR-0009(runtime 进沙箱)、ADR-0008 §3 / ADR-0012(warm pool 改走
  节点镜像预拉)、ADR-0002(SandboxProvider 铁律)
- 关联任务：#15;前置数据 #14(`bench/README.md` §3.2)
- 日期：2026-06-17

## 背景

#15 收尾两件互相咬合、都卡在「需带 gVisor 真集群」这道门上的事:gVisor(runsc)
兼容性验收(ADR-0008/0009 的 pre-prod 门),与 K8s warm-pool 落地(按 ADR-0012
改走节点镜像预拉,而非不可行的 adopt-by-remount)。本提交仅落地 Plan 文档,先于
编码,遵循「编码前先出 Plan」约束。

## 改动

- `docs/plan/hardening-gvisor-spike-and-image-warmer.md`(新增):
  - §1 目标与动机(含 #14 冷启动数据表)、§2 复用不造轮子(RuntimeClass/预拉
    DaemonSet/镜像变量/压测均复用既有资产)、§3 现状勘定、§4 设计(节点预拉
    DaemonSet + runsc 五项 compat 探针)、§5 提交分层(S1 清单 / S2 脚本 /
    S3 条件触发的 provider 修 / S4 真集群验收)、§6 风险、§7 验收标准。
  - 明确分层:Layer A/B 本机可做并静态校验,Layer C(真集群 + gVisor 端到端
    + 冷启动复测)待目标集群,不阻塞 A/B 合并。

## 范围说明

本提交仅 Plan 文档,无代码/部署物改动。S1(image-warmer DaemonSet + Helm 模板)
起进入实现。
