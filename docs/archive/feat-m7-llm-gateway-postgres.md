# feat(m7): llm-gateway 账本/配额落 Postgres + Redis 快路径

M7 持久化第四步:llm-gateway 的计费账本(usage ledger)与配额计数
(quota counters)落 Postgres,做到重启不丢;Redis 退化为高频计数的快路径镜像。
未配置 `COCOLA_PG_DSN` 时维持原有 Redis/内存行为,保零依赖 dev 启动。

## 改动

- `apps/llm-gateway/cocola_llm_gateway/billing/postgres_ledger.py`(新增):
  `PostgresLedger` 实现 `Ledger` Protocol,写 `usage_ledger` 表
  (`ON CONFLICT (request_id) DO NOTHING`,幂等摄入);聚合走 `SUM`/`COUNT`
  冷读,不维护第二套计数器避免与行真相漂移。连接池 `AsyncConnectionPool`
  懒打开,使同步组合根可在无事件循环时构造。账本是计费真相,无 TTL。
- `apps/llm-gateway/cocola_llm_gateway/quota/postgres_store.py`(新增):
  - `PostgresQuotaStore` 实现 `QuotaStore` Protocol,`quota_counters` 表
    `INSERT ... ON CONFLICT DO UPDATE` 原子累加,行持久无 TTL。
  - `MirroredQuotaStore`:Redis 快路径叠加 PG 持久层。`add` 先写 PG(权威总数
    不滞后)再尽力镜像 Redis;`get` 读 PG 权威总数 —— 写序保证 durable >= fast,
    故 Redis 冷启/被清空也绝不少报已用额度(重启后预算不被悄悄清零)。
- `cocola_llm_gateway/bootstrap.py`:`build_ledger` / `build_quota_store` 新增
  `COCOLA_PG_DSN` 选择;PG+Redis → 镜像,PG only → 持久,Redis only / 无 → 原行为。
- `billing/__init__.py`、`quota/__init__.py`:导出新后端。
- `pyproject.toml`:新增 `psycopg[binary]>=3.1`、`psycopg-pool>=3.2`(uv 管理)。
- `apps/llm-gateway/tests/test_postgres_parity.py`(新增):Memory 与 Postgres
  跑同一 `Ledger`/`QuotaStore` 契约;PG 腿由 `COCOLA_TEST_PG_DSN` 门控,未设即 skip。
  含镜像「durable 权威读」回归(冷快路径不少报)。

## 测试

- `uv run pytest`(忽略需生成 protobuf 的既有 e2e 模块)101 passed, 3 skipped。
- Postgres 对拍腿:对真实 postgres:16-alpine(应用 db/migrations schema v1)实跑,
  `test_postgres_ledger_parity` / `test_postgres_quota_parity` /
  `test_mirrored_quota_reads_durable_truth` 全过。
- bootstrap 后端选择三路径(PG only / PG+Redis 镜像 / Redis only)实测正确。
- ruff check / format 对新增文件全绿。
