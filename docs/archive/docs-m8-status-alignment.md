# docs(m8):路线图 / ADR 状态与现状对齐

## 背景

M8 六步(S1–S6)全部落地后,部分文档状态滞后于代码现状,做一次勘误对齐,
避免文档与实现不一致误导后续读者。

## 改动

- `README.md`:路线图 M8 由 ⏳ 改 ✅,并补全条目内容(五服务 RED 指标 + OTel
  链路默认关 + 部署观测栈 + k6/ghz 压测套件与容量基线 runbook)。
- `docs/adr/0008-persistence-layering-and-vault.md`:Status `Proposed` → `Accepted`
  (持久化分层与 K8s/gVisor 后端已随 M6/M7 落地;Vault 密钥托管仍留待后续),
  补记 accepted 日期。
- `docs/adr/README.md`:索引中 0008、0009 两行 `Proposed` → `Accepted`,与各
  ADR 正文一致(0009 正文此前已是 Accepted,仅索引滞后)。

## 说明

纯文档勘误,无代码 / 行为变更。
