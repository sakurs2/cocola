# 变更记录：S3 —— Vault 机密托管文档收口

- 关联 Plan：`docs/plan/hardening-vault-secret-management.md`（S3 阶段）
- 关联 ADR：ADR-0008 §5、Status、Follow-ups
- 日期：2026-06-15

## 改动

- `docs/runbook/secrets-vault.md`（新增）：Vault 机密托管 runbook —— `*_FILE`
  间接约定、Vault KV path 布局、集群侧一次性前置(装 Injector / k8s auth /
  写机密+policy+role)、启用注入步骤、dev→prod 迁移、轮换说明、验收与边界。
- `docs/adr/0008-persistence-layering-and-vault.md`：
  - Status 行更新为「集成方式已定并落地代码侧 + K8s 清单 (2026-06-15)，真链路
    注入待目标集群」。
  - §5 追加「实现进展 (2026-06-15)」段：明确集成方式为 `*_FILE` 间接 +
    Vault Agent Sidecar，记录代码缝口与 K8s 清单落地、runbook 位置。
  - Follow-ups 的 Vault 项标注「代码缝口 + K8s 注入清单已落地，真链路验收待集群」。

## 验收

- 纯文档变更,无代码/构建影响。
- 与 S1(代码缝口)、S2(K8s 清单)共同收口 ADR-0008 §5 的 Vault 集成方式决策。

## 范围说明

至此 Vault 机密托管的代码侧 + 部署清单 + 文档三件套齐备。真链路注入验收
(需装 Vault + Injector 的目标 Linux 集群)与 #14/#15 同批,留待后续。
