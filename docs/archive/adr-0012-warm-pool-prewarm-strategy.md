# 变更记录：ADR-0012 —— warm pool 在 PVC/bind-mount 卷模型下的预热策略修订

- 关联 ADR：ADR-0012(新增)、ADR-0008 §3(修订)、ADR-0002(铁律)
- 关联任务：#13(warm pool 引擎已落地)、#15(K8s 落地 + 实测)
- 日期：2026-06-17

## 背景

#13 落地 warm pool 引擎时,把 ADR-0008 §3 的「bind on demand」实现为可选能力
`provider.Adopter`——领用一个 user-agnostic 预热箱再后挂用户卷。核对两个 backend
后确认这条路在 Docker(bind-mount 固定在 ContainerCreate)和 K8s(Pod spec
volumes 创建后不可变)上**都不可行**。原 ADR 的措辞隐含了一个现有 backend 都不
具备的能力,需正式收口为决策,避免 #15 在 K8s 上重蹈覆辙。

## 改动

- `docs/adr/0012-warm-pool-prewarm-strategy-under-pvc-volume-model.md`(新增):
  - Context:解释 adopt-by-remount 为何在 PVC/bind-mount 模型上不成立;澄清
    K8s+gVisor 冷启动大头是 1.9GB 镜像拉取而非挂卷。
  - Decision:放弃 adopt-by-remount 主路径;`provider.Adopter` 缝口保留但承认
    当前无 backend 实现(静默降级 cold create);K8s warm pool 改走「DaemonSet
    节点镜像预拉」。
  - Alternatives:A 热挂 PVC(不可变,否决)/ B 挂 userdata 根(越权,否决)/
    C cp 拷贝(造轮子+破坏持久化语义,否决)/ D StatefulSet(模板不可变,否决)/
    E 节点镜像预拉(选定)。
- `docs/adr/0008-persistence-layering-and-vault.md`:§3 warm pool 段落修订,
  「bind on demand」改为「node image warming 为主路径;adopt-by-remount 需
  backend 支持卷热挂,当前 Docker/K8s 均不支持」,指向 ADR-0012。
- `docs/adr/README.md`:索引追加 0012 行。
- `README.md`:路线图补 WP(warm pool 引擎已落地)与 GV(gVisor spike + K8s
  warm-pool 节点预拉)两行,填补此前停在 M8 的缺口。

## 范围说明

本次为纯文档/决策收口,不含代码改动;#13 已落地的引擎与降级逻辑无需回退。
K8s 节点镜像预拉清单 + runsc 冷启动实测随 #15 落地。
