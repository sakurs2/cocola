# 变更归档:#15 S2 — verify-gvisor.sh(runsc 兼容性 + 冷启动复测脚本)

- 任务:#15 gVisor (runsc) 兼容性 spike + K8s warm-pool 节点镜像预拉
- 阶段:S2(runsc 兼容性 + 冷启动复测脚本,本机仅 dry-run / `bash -n`)
- 关联:Plan `docs/plan/hardening-gvisor-spike-and-image-warmer.md` §4.2、ADR-0008 §3、ADR-0012、bench/README §3.2/§3.4

## 背景

gVisor(runsc)是 cocola 可选的更强「应用内核」隔离(默认是 runc + user namespace,零节点级安装)。Node + Claude Code 是重运行时,runsc 在用户态拦截 syscall,部分 syscall 不支持 —— 因此在推荐启用 runsc 前,必须先在真集群上证明 Route-A 在 runsc 下确实健康。本阶段交付这套验收脚本。

## 本阶段改动

1. `deploy/k8s/verify-gvisor.sh`(新增,可执行)
   - 在装好 gVisor shim + 应用了 `runsc` RuntimeClass 的真集群上,对一个 runsc-backed 的探针 Pod 跑 6 个 compat 探针,逐项 pass/fail/skip 并汇总判定;有 FAIL 则 `exit 1`(导向 #15 S3)。
   - 探针与 Plan §4.2 一一对应:
     1. toolchain:`node --version` / `claude --version` 在 runsc Pod 内退出 0;
     2. egress:经 gateway 的一次真实 query 跑通(验证 NetworkPolicy 与 runsc 网络栈不打架),`RUN_EGRESS=1` 开启;
     3. io:`/workspace` 与 `~/.claude` 的原生 bash + 文件 IO;
     4. resume:hibernate(删 Pod 留 PVC)→ 重建重挂 → `claude --resume` 续会话(disk-kept,ADR-0008),需 `PVC=<name>`;
     5. coldstart:逐请求冷启计时,「开/关节点镜像预拉」两组对照,量化 runsc + 1.9GB 镜像冷启与预拉收益,`RUN_COLDSTART=1` 开启;
     6. checkpoint:runsc checkpoint/restore 探针(吸收 Agent Substrate 思路),验证我们的镜像能否做「连 RAM 一起保留」的 resume,通过则可把 ADR-0008 §3 从 RAM-lost 升级为 RAM-kept;`RUN_CHECKPOINT=1` + `RUNSC_CID=<cid>`(须在节点上跑)。
   - 工程约束:`set -euo pipefail`;`DRY_RUN=1` 只打印不变更(本机可验逻辑);全程经 `kubectl exec -i` STDIO 驱动 Pod 内 CLI,**绝不监听端口**;`trap cleanup EXIT` 清理探针 Pod;探针顺序把「无网络」项(1、3)排在 egress(2)前,gateway 缺失不阻断核心隔离判定。
   - 默认行为:无标志时只跑探针 1/3/4(4 需 PVC,否则 skip);2/5/6 默认 skip 并打印开启方式。

2. `bench/README.md`(修改,§3.4)
   - 在「K8s + gVisor 基线」缺口项补充指向 `deploy/k8s/verify-gvisor.sh` 探针 5(`RUN_COLDSTART=1`,开/关预拉两组对照),并约定跑通后把 runsc 冷启数据 + 预拉收益回填 §3.2 表。

## 复用,不造轮子

- 复用既有脚本约定(`set -euo pipefail`、env knobs 注释块、`command -v` 守卫、CI-smoke 风格),与 `scripts/sandbox-runtime-verify.sh`、`bench/ghz/agent_query.sh` 同构。
- 复用既有资产:`runsc` RuntimeClass(`deploy/k8s/01-runtimeclass.yaml`)、`sandbox.image`、bench §3.2 基线作为对照基准。
- 探针 6 仅借鉴 Agent Substrate 的 checkpoint/restore 技术路线,不引入其本体(整体集成评估见独立 task #17)。

## 静态校验(本机,无集群/无 gVisor)

- `bash -n deploy/k8s/verify-gvisor.sh`:语法干净。
- `DRY_RUN=1 RUN_EGRESS=1 RUN_COLDSTART=1 RUN_CHECKPOINT=1 PVC=test-pvc bash deploy/k8s/verify-gvisor.sh`:6 个探针全部按预期渲染意图、互不阻断,verdict `PASS=0 FAIL=0 SKIP=6`,退出 0;探针 1 的 dry-run 还原出了正确的 runsc 探针 Pod 清单(runtimeClassName=runsc、sandbox 镜像、sleep)。
- 说明:本机为 macOS,无真 runsc,真实探针执行(Layer C)留待目标集群。

## 不在本阶段(后续)

- S3:若 Layer C 暴露 runsc 兼容坑 → k8s provider 最小修 + 单测(fake clientset)。
- S4:真集群跑 verify-gvisor.sh 全绿;冷启动复测数据回填 bench §3.2;ADR-0008/0012 Status 更新为「真集群已验收」;若探针 6 通过,评估 RAM-kept resume 并回写 ADR-0008 §3。
