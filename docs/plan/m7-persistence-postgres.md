# Plan: M7 持久化(Postgres)—— 让会话/账本/控制面数据可重启存活

> 状态:待评审(review-before-code)。本文只描述方案,不含实现。

## 1. 目标与动机

当前所有业务状态都是**进程内存 / Redis**,容器一重启历史即丢:

- `admin-api`:令牌元数据、吊销名单、配额覆盖、Skill 目录、审计日志 —— 全在
  in-memory `Store`(代码注释已写明 "a PostgreSQL implementation lands in M7")。
- `llm-gateway`:计费账本(usage ledger)与配额计数 —— Memory 或 Redis。
- `agent-runtime`:`session → session_id` 映射在**进程内 dict**(用于 --resume),
  重启即丢,沙箱里的对话无法续接。

M7 的目标:把**需要长期存活的业务数据**落到 Postgres,做到"重启不丢、可自托管、
多副本可共享",兑现 cocola "企业级可自托管" 的定位。

本方案严格落地 ADR-0008 的持久化分层(T1/T2/T3),不另起炉灶。

## 2. 范围

### 做(本里程碑)

1. **基础设施**:`docker-compose.full.yml` 引入 `postgres:16-alpine`(dev compose
   已有,复制过来 + healthcheck + 卷),各服务加 `COCOLA_PG_DSN`。
2. **admin-api(Go)**:在已有 `store.Repository` interface 后新增 **Postgres 实现**
   (token 元数据 / 吊销 / 配额覆盖 / skill 目录 / 审计日志),与 in-memory 实现
   行为一致,服务/handler 零改动(seam 已就绪)。
3. **llm-gateway(Py)**:usage ledger + 配额计数落 Postgres(账本是计费真相,
   必须持久);保留 Redis 作高频计数/限流的快路径(读写穿透到 PG)。
4. **agent-runtime(Py)**:`session → session_id` 映射落持久存储,使沙箱对话在
   重启后仍可 `--resume`。
5. **迁移**:引入轻量迁移机制,首版 schema 入库;CI/启动时可幂等执行。

### 不做(后续里程碑)

- MinIO/对象存储(T2 文件版本)、Vault(T3 密钥)—— 仍按 ADR-0008 留到 M6/后续。
- 生产鉴权开启、egress allowlist —— 独立任务。
- 用户卷(PVC / bind-mount)的 `~/.claude` 持久化 —— 随 M6 落地。

## 3. 选型(优先复用开源,符合项目原则)

| 关注点 | 选型 | 理由 |
|---|---|---|
| 数据库 | PostgreSQL 16 | ADR-0001/0008 已定;dev compose 已在用 |
| Go 驱动 | `jackc/pgx/v5`(pgxpool) | Go PG 事实标准,无 ORM 负担,贴合现有手写 SQL 风格 |
| Go 迁移 | `golang-migrate` 或 `goose`(二选一,倾向 goose:可内嵌) | 纯 SQL 迁移,自托管友好 |
| Py 驱动 | `psycopg[binary]>=3`(async) | 与 FastAPI async 栈契合;uv 管理 |
| Py 迁移 | 复用同一套 SQL 迁移文件(由 admin-api 或独立 migrator 容器统一执行) | 避免两套 schema 真相 |
| 连接管理 | 各服务连接池 + 启动时迁移幂等检查 | — |

> 说明:Py 侧刻意不引 SQLAlchemy/Alembic(避免重 ORM 与第二套迁移真相);
> ledger/quota 是少量明确的表,手写 SQL + psycopg 足够,符合"避免造轮子也避免过度
> 抽象"的平衡。**此点请你拍板**:是否接受"SQL 迁移文件单一真相 + 双语言各自只读
> 执行"的方案。

## 4. 数据模型(首版 schema 草案)

按服务归属拆 schema,放在各自的迁移目录,统一库 `cocola`:

- **admin 域**
  - `token_records`(id PK, user_id, tenant_id, issuer, issued_at, expires_at,
    revoked, revoked_at, created_by)
  - `quota_overrides`(scope, subject, period, limit_tokens, ...; (scope,subject) 唯一)
  - `skill_entries`(id PK, name, ..., 时间戳)
  - `audit_log`(id PK, ts, actor, action, target, detail JSONB)
- **gateway 域**
  - `usage_ledger`(id PK, ts, user_id, tenant_id, model, input_tokens,
    output_tokens, cost_usd, request_id)
  - `quota_counters`(subject, period_key, used_tokens; 唯一键;Redis 为快路径镜像)
- **agent-runtime 域**
  - `session_map`(session_id PK, claude_session_id, user_id, sandbox_id,
    updated_at)

索引:按 user_id / tenant_id / period_key / ts 常用查询建二级索引。

## 5. 迁移与落地步骤(建议顺序)

1. compose 加 postgres + 卷 + healthcheck;各服务注入 `COCOLA_PG_DSN`(dev 默认值)。
2. 落 SQL 迁移文件(schema v1)+ 选定迁移执行器;启动时幂等 apply。
3. admin-api:PG 实现 `Repository`,加集成测试(testcontainers 或 dev PG),
   与 in-memory 对拍行为一致;env 开关选择后端(默认 memory,设 DSN 即切 PG)。
4. llm-gateway:ledger/quota 落 PG;Redis 退化为计数快路径;补测。
5. agent-runtime:session_map 落 PG;重启后 --resume 续接验证。
6. 端到端:重启整栈后历史令牌/账本/会话仍在;全量测试 + ruff/gofmt 全绿。

## 6. 验收标准

- `docker compose ... down` 后再 `up`(保留 pg 卷),令牌列表、账本、配额、会话
  映射**全部存活**。
- 各服务"未配置 DSN → in-memory(零依赖 dev 启动不破)",配置 DSN → PG,**同一
  套 interface,无 handler 改动**。
- admin-api PG 实现与 in-memory 实现通过同一组对拍测试。
- Web 端:重启后用同一 session_id 继续对话,上下文续接(--resume 生效)。

## 7. 风险与回滚

- **双语言迁移一致性**:用单一 SQL 真相规避;若执行器分歧,退化为"独立 migrator
  容器统一 apply"。
- **零依赖 dev 启动不能破**:默认 memory 后端保留,DSN 存在才切 PG。
- 回滚:不设 DSN 即回到当前内存态;schema 迁移提供 down。

## 8. 已定决策(2026-06-11)

1. **Py 侧迁移**:采用「SQL 单一真相 + psycopg 只读执行」,**不引 Alembic/SQLAlchemy**。
2. **Go 迁移器**:采用 **`goose`**(可内嵌、单二进制友好,自托管干净)。
3. **session_map 纳入本期**:agent-runtime 的 `session → claude_session_id` 映射
   一并落 PG,确保"重启后对话可续接(--resume)"这一最直观体验不缺。

落地顺序:统一以 `db/migrations/*.sql`(goose 格式)为 schema 单一真相;Go 侧用
goose 库内嵌执行迁移,Py 侧仅用 psycopg 连接、**不重复定义 schema**(只读执行/查询)。
