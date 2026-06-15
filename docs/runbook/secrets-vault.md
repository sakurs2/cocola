# Runbook：Vault 机密托管

适用：cocola 平台机密(T3)从明文 env 迁入 HashiCorp Vault。本文覆盖 path 约定、
Vault Agent 注入用法、`*_FILE` 间接约定、dev→prod 迁移与轮换。

关联：ADR-0008 §5、`deploy/k8s/06-vault-secrets.yaml`、
`docs/plan/hardening-vault-secret-management.md`。

## 0. 设计一句话

应用**不连 Vault、不带 Vault SDK**。Vault Agent Sidecar 把机密渲染成**文件**,
应用经 `*_FILE` 间接缝口读文件。复用成熟集成,符合 ADR §5「no custom crypto」。

```
Vault KV  --(Agent Sidecar 渲染)-->  /vault/secrets/<name>  --(COCOLA_<NAME>_FILE 指向)-->  应用读文件
                                                              └ 未注入时 *_FILE 不存在 → 回落同名 env
```

## 1. `*_FILE` 间接约定(应用侧,已实现)

任意 `COCOLA_*` 机密都支持「文件优先」:

- 若设了 `COCOLA_<NAME>_FILE`,程序读该路径文件内容(去尾换行)作为机密值；
- 否则回落到 `COCOLA_<NAME>` env；
- 文件读失败(挂载瞬时缺口)也降级到 env,不崩。

实现:Go `packages/go-common/config.SecretFromEnv`(gateway/admin-api/admin-mint
复用);Python `cocola_llm_gateway.config.read_secret_env`(`_resolve_secret` 与
`auth_config_from_env` 走它,`issue_token.py` 传递性覆盖)。

覆盖机密:`COCOLA_AUTH_SECRET`(全链路 HS256)、`COCOLA_ADMIN_KEY`(admin-api)、
上游 `COCOLA_ANTHROPIC_API_KEY` 等。

## 2. Vault path 布局

```
secret/cocola/platform/        # 平台级 T3(全租户共享)
  auth_secret        -> COCOLA_AUTH_SECRET
  admin_key          -> COCOLA_ADMIN_KEY
  anthropic_api_key  -> 上游模型 key
secret/cocola/users/{user_id}/ # 用户级(每用户隔离,按需)
  <name>
```

渲染目标统一 `/vault/secrets/<name>`,应用侧 `COCOLA_<NAME>_FILE` 指向之。

## 3. 集群侧前置(一次性)

需可访问 Vault 的运维机执行(本仓不含,沙箱内禁起 Vault server):

1. 装 Vault + Agent Injector:`helm install vault hashicorp/vault \
   --set injector.enabled=true`。
2. 启用 k8s auth:`vault auth enable kubernetes`,并配 `auth/kubernetes/config`。
3. 写机密 + policy + role(样例见 `06-vault-secrets.yaml` 的 ConfigMap
   `bootstrap.md` / `policy-samples.hcl`):

```
vault kv put secret/cocola/platform/auth_secret value="$(openssl rand -hex 32)"
vault kv put secret/cocola/platform/admin_key value="<your-admin-key>"
vault kv put secret/cocola/platform/anthropic_api_key value="<sk-ant-...>"
vault policy write cocola-platform-ro - <<'HCL'
path "secret/data/cocola/platform/*" { capabilities = ["read"] }
HCL
vault write auth/kubernetes/role/cocola-platform \
  bound_service_account_names=cocola-platform \
  bound_service_account_namespaces=cocola \
  policies=cocola-platform-ro ttl=1h
```

## 4. 启用注入(部署侧)

`kubectl apply -f deploy/k8s/06-vault-secrets.yaml`(在 00~05 之后)。它:

- 建 `ServiceAccount/cocola-platform`(绑定 Vault role);
- 用同名 `Deployment/llm-gateway` 叠加 `vault.hashicorp.com/agent-inject` 注解,
  渲染 `auth_secret` / `anthropic_api_key` 到 `/vault/secrets/*`,并设
  `COCOLA_*_FILE` 指向之；
- 保留原生 `secretKeyRef` 为未启用 Vault 时回落。

gateway / admin-api 不在本仓 `deploy/k8s/` 范围;部署它们时照搬文件 (c) 段的
注解模板(admin-api 另加 `admin_key`)。

## 5. dev → prod 迁移

- **dev(本机)**:不设任何 `_FILE`,沿用 `.env` 明文(`.env.example` 已注明),
  行为与历史逐字一致。
- **prod**:启用 Vault Agent 注入,机密只活在 Vault + tmpfs 渲染文件,容器 env
  里不再常驻明文(原生 secretKeyRef 仅作未启用 Vault 的回落)。

## 6. 轮换

在 Vault 改值即可:`vault kv put secret/cocola/platform/auth_secret value=...`。
Vault Agent 按 TTL 重渲染文件;滚动重启 Pod 让进程重读(`*_FILE` 在进程启动时读,
当前不热重载)。`COCOLA_AUTH_SECRET` 为全链路共享密钥,轮换需三方(gateway /
admin-api / llm-gateway)同步重启,避免签发/校验跨密钥。

## 7. 验收与边界

- 本机:`06-vault-secrets.yaml` 离线 YAML 解析通过;S1 单测(Go + Python)证
  `*_FILE` 优先、env 回落、尾换行 strip。
- 真链路注入(需装 Vault+Injector 的目标集群)与 #14/#15 同批待 Linux 集群。
- 不连 Vault API、不引入 Vault SDK;不在沙箱内起 Vault server。
