# 变更记录：S2 —— Vault 部署清单(Vault Agent 注入)

- 关联 Plan：`docs/plan/hardening-vault-secret-management.md`（S2 阶段）
- 关联 ADR：ADR-0008 §5、§7 步骤 4
- 日期：2026-06-15

## 背景

S1 已落地 `*_FILE` 机密间接缝口(应用零感知 Vault)。S2 给出在 K8s 上用
HashiCorp Vault Agent Injector 把机密渲染成文件、并用 `COCOLA_*_FILE` 指向它的
部署清单 —— 复用成熟集成,不引入 Vault SDK(用户约束 + ADR §5)。

## 改动

- `deploy/k8s/06-vault-secrets.yaml`（新增）：可选叠加层,含三部分——
  - (a) `ConfigMap/cocola-vault-conventions`：纯文档,给出 Vault KV path 布局
    (`secret/cocola/platform/*`、`users/{id}/*`)、最小权限 policy 样例、
    一次性 bootstrap 命令(写机密 + policy + k8s role)。
  - (b) `ServiceAccount/cocola-platform` + llm-gateway 注入式 `Deployment`
    (与 05 同名,后应用即叠加):加 `vault.hashicorp.com/agent-inject` 系列
    注解,渲染 `auth_secret` / `anthropic_api_key` 到 `/vault/secrets/*`,并设
    `COCOLA_*_FILE` 指向之；原生 `secretKeyRef` 保留为未启用 Vault 时的回落
    (`_FILE` 文件不存在则 S1 缝口自动回落到同名 env)。
  - (c) gateway / admin-api 的注入注解模板(注释样例)：这两个平面不在本仓
    `deploy/k8s/` 范围,部署时照搬片段(admin-api 另加 `admin_key`)。
- `.env.example`：补充 `*_FILE` 文件间接用法说明与 Vault 渲染示例(不放真值)。

## 验收

- `deploy/k8s/06-vault-secrets.yaml` 经离线 YAML 解析校验通过：3 个文档
  (ConfigMap / ServiceAccount / Deployment),注入式 Deployment 含
  `COCOLA_AUTH_SECRET_FILE` / `COCOLA_ANTHROPIC_API_KEY_FILE` env、
  `agent-inject: "true"` 注解、`serviceAccountName: cocola-platform`。
- dev `.env` 流回归：不设 `_FILE` 时行为与现状逐字一致(S1 已证)。

## 范围说明

K8s 真链路注入验收(需装 Vault + Injector 的目标集群)与 #14/#15 同批待
Linux 集群；本机仅完成清单编写与静态校验。S3(runbook + ADR Status)后续独立提交。
