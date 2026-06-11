# docs(plan): M7 持久化(Postgres)方案 + 决策定稿

为 M7 持久化里程碑撰写方案并定稿三项关键决策。

## 改动

- `docs/plan/m7-persistence-postgres.md`(新增):
  - 现状:业务状态全在内存/Redis,重启即丢(admin-api 令牌/吊销/配额/skill/
    审计、gateway 账本/配额、agent-runtime 进程内 session 映射)。
  - 范围:compose 引入 postgres;admin-api/gateway/agent-runtime 三处落 PG;
    默认仍 memory,设 DSN 才切 PG(保零依赖 dev 启动)。MinIO/Vault/用户卷留后续。
  - 选型:Go=pgx/v5,Py=psycopg3(uv,不引 Alembic),SQL 迁移单一真相。
  - 首版 schema 草案 + 落地步骤 + 验收 + 风险。
  - 已定决策:① SQL 单一真相 + psycopg 只读;② goose 迁移器;③ session_map
    纳入本期。

## 测试

- 纯文档,无代码改动。
