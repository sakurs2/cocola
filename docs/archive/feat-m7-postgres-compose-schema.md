# feat(m7): Postgres compose 节点 + schema v1 迁移(goose)

M7 持久化第一、二步:全栈引入 Postgres 数据节点,并落定 v1 关系 schema。

## 改动

- `deploy/docker-compose/docker-compose.full.yml`:
  - 新增 `postgres`(postgres:16-alpine,user/pw/db = cocola/cocola_dev_pw/cocola,
    `pgdata` 卷,`pg_isready` healthcheck,映射 5432)。
  - admin-api / llm-gateway / agent-runtime 三服务新增 `depends_on: postgres
    (service_healthy)` 与 `COCOLA_PG_DSN`(默认指向本栈 postgres)。
  - 顶层 `volumes` 增加 `pgdata`。
  - 设计不变量:DSN 未设置时各服务仍走内存后端,保证零依赖 dev 启动;全栈
    默认提供 DSN 即切 PG。
- `db/migrations/00001_init_schema.sql`(新增,goose 格式,单一真相):
  - admin 域:token_records / quota_overrides / skill_entries / audit_log。
  - gateway 域:usage_ledger / quota_counters。
  - agent-runtime 域:session_map。
  - 按 user_id / tenant_id / period_key / ts 建二级索引;Up/Down 均提供。
  - 列定义对齐各服务现有数据模型(store.go 结构体、billing UsageRecord、
    quota 计数键、shim_provider session 映射)。

## 测试

- compose 配置校验:`docker compose -f docker-compose.full.yml config --quiet` 通过。
- SQL 迁移针对真实 postgres:16-alpine 实跑校验:Up 建出 7 张表 + 全部索引;
  Down 干净回滚(无残留 relation)。校验容器用后即焚。
