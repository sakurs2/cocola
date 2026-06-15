# Plan: 密钥托管 —— Vault 集成（机密迁出明文）

- 关联：ADR-0008 §5「Secrets via Vault, using mature integrations (no custom
  crypto)」、§7 步骤 4、Status 行「Vault 密钥托管留待后续」、Consequences 的
  「Vault Sidecar/CSI wiring + converge secrets」。
- 核心原则（用户约束 + ADR）：**复用成熟集成、绝不自造加密、不给应用塞 Vault
  SDK 依赖**。Vault Agent Sidecar / CSI 的精髓是「应用无感」——机密被渲染成
  **文件或 env**，应用照常读取。本机（macOS）可完整完成与测试代码侧,K8s 运行时
  集成只能在本机**编写清单**,真链路验收待目标集群（与 #14/#15 同属待 Linux）。

## 现状勘定（已查）

机密当前如何被读取:

| 机密 | 读取方 | 现状 |
|---|---|---|
| `COCOLA_AUTH_SECRET`（HS256 全链路签名） | gateway(Go) / admin-api(Go) / llm-gateway(Py) | 三处均 `os.Getenv` / `os.getenv` 明文 env |
| 上游 `COCOLA_ANTHROPIC_API_KEY` 等 | llm-gateway `config.py:_resolve_secret` | 已走 `api_key_env` env 间接,**密钥不进配置文件** |
| `COCOLA_ADMIN_KEY` | admin-api(Go) | 明文 env |
| 本地 dev | `.env`（已 .gitignore） | `local-dev-secret` 等占位 |
| K8s | `deploy/k8s/05-*.yaml` | 已用原生 `secretKeyRef`（cocola-llm Secret） |

**结论**:代码侧已大半 Vault-ready（全部走 env / env 间接）。缺口是
①机密仍可能以明文长驻容器 env;②无「从文件读机密」的标准缝口让 Vault Agent /
CSI / Docker secret 注入;③无 Vault 部署清单与 runbook。

## 设计抉择

**不引入 Vault SDK,改用业界标准的 `*_FILE` 间接约定**(Postgres 官方镜像、
Docker secrets、Vault Agent template 渲染均用此约定):

- 读机密时优先看 `<NAME>_FILE`:若设,从该路径读取文件内容(strip 尾换行);
  否则回落到 `<NAME>` env;再否则空/inline。
- 这样 Vault Agent Sidecar 把 `secret/cocola/platform/auth_secret` 渲染到
  `/vault/secrets/auth_secret`,只需令 `COCOLA_AUTH_SECRET_FILE` 指向它,**零应用
  代码感知 Vault**。CSI SecretProviderClass、Docker secret 同理复用同一缝口。
- dev `.env` 流不变(不设 `_FILE` 即用 env,与现状完全一致)。

> 为何不直接连 Vault API:那会给每个服务塞 hashicorp/vault SDK 依赖、要管
> token 续租/审计,正是 ADR §5 与「不造轮子」要避免的。`_FILE` + Vault Agent
> 把这些交给成熟 sidecar,应用只读文件。

## 改动清单

### S1 代码:`*_FILE` 机密间接缝口（本机可完整测试）
1. **Python（llm-gateway）**:在 `config.py` 加 `_read_secret_env(name)`
   helper（先 `<name>_FILE` 文件、再 `<name>` env）。
   - `_resolve_secret` 的 env 分支改走它(上游 key 支持文件注入)。
   - `config.py` 读 `COCOLA_AUTH_SECRET` 处改走它。
   - 加单测 `test_secret_indirection.py`:文件优先、env 回落、都缺为空、
     尾换行被 strip。
2. **Go（gateway / admin-api）**:在共享包(找现有 `internal/`/`pkg` 公共处,
   无则建最小 `internal/secret`)加 `secret.FromEnv(name) string`,同样
   `<name>_FILE` 优先。替换 3 处 `os.Getenv("COCOLA_AUTH_SECRET")` 与
   `os.Getenv("COCOLA_ADMIN_KEY")`。加 `_test.go`。
   - 注意 Go module 边界:gateway / admin-api 各自 module,helper 若不便共享则
     各放一份极小实现(15 行,避免跨 module 依赖,符合现有布局)。

### S2 部署清单:Vault 集成（本机编写,真链路待集群）
3. `deploy/k8s/` 新增 `06-vault-secrets.yaml`(或扩 helm):
   - **方案 A(主推)Vault Agent Sidecar injection**:给 gateway/admin-api/
     llm-gateway Deployment 加 `vault.hashicorp.com/agent-inject` 系列 annotation
     + `agent-inject-template-*`,渲染 `secret/cocola/platform/*` 到
     `/vault/secrets/*`,并设 `COCOLA_*_FILE` 指向之。
   - 保留原生 `secretKeyRef` 作为未启用 Vault 时的回落(注释说明二选一)。
   - 附 `secret/cocola/platform/*` 与 `users/{id}/*` 的 path 约定 + 最小 policy
     (least-privilege)样例。
4. `.env.example` 注释补充 `*_FILE` 用法与 Vault 渲染示例(不放真值)。

### S3 文档
5. `docs/runbook/secrets-vault.md`:path 布局、Vault Agent annotation 用法、
   `_FILE` 约定、dev→prod 迁移、轮换说明。
6. ADR-0008:§5 标注「集成方式已定(Vault Agent Sidecar + `_FILE` 间接)」、
   Status 行更新、Consequences 勾掉对应项。
7. `docs/archive/` changelog。

## 验收
- llm-gateway:`uv run ruff` + `uv run pytest`(含新 secret 间接单测)全绿。
- gateway / admin-api:`GOWORK=off go build/vet/test ./...`(各自 module)全绿。
- `helm template` / `kubectl --dry-run=client`(若本机有 kubectl)对新清单做
  静态校验;无则人工 review YAML。
- dev `.env` 流回归:不设 `_FILE` 时行为与现状逐字一致。

## 不做（明确划界）
- 不连 Vault API、不引入 Vault SDK 依赖。
- 不在沙箱内起 Vault server(网络监听禁令);真链路注入验收待目标集群(与
  #14/#15 同批)。
- 不改 dev 默认(auth off / fake upstream)。

## 回滚
- S1/S2/S3 各自独立 commit,可分别 revert。`_FILE` 缝口向后兼容(不设即旧行为)。
