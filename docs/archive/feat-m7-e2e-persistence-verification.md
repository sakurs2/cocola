# feat(m7): 端到端持久化验收 —— 重启整栈数据全部存活

M7 持久化第六步(收尾验收):在 `docker-compose.full.yml` 全栈(postgres +
redis + 六个服务)上验证 ADR-0008 T2 的核心承诺——**容器重启后业务数据不丢**,
并跑通全量测试 + lint。本步不含代码改动,只做端到端验收与文档收尾。

## 验收过程

栈以 `COCOLA_DATA_ROOT` 指向宿主目录、`COCOLA_PG_DSN` 注入各服务方式启动。
启动日志确认三服务均选中 PG 后端:

- admin-api:`persistence backend: postgres`(goose 迁移至 version 1,8 张表就绪)
- llm-gateway:`billing ledger: postgres` + `quota store: postgres(durable)
  + redis(fast-path mirror)`
- agent-runtime:`session-map: Postgres(durable resume index)`

六张业务表各写入种子数据(quota_overrides / skill_entries 经 admin-api 真实
HTTP API 写入;token_records / usage_ledger / quota_counters / session_map 因
令牌签发 API 在无 `COCOLA_AUTH_SECRET` 时禁用,改由 SQL 直插)。

随后执行**保留卷重启**:`docker compose down`(不带 `-v`,`cocola_pgdata`
卷保留)→ `up -d`。重启后逐表复核:

- token / quota / skill —— 经 admin-api GET 端点读回(证明服务从 PG 读,非仅
  DB 残留):令牌列表、配额列表、Skill 详情均原样返回。
- usage_ledger / quota_counters / session_map —— SQL 复核行数,全部存活。
- 六张表行数在重启前后一致(各 1 行),零丢失。

对应验收标准:「`docker compose down` 后再 `up`(保留 pg 卷),令牌列表、账本、
配额、会话映射**全部存活**」——通过。

## 全量测试 + Lint

- agent-runtime:`uv run pytest` 50 passed(含 `COCOLA_TEST_PG_DSN` 门控的
  PG 对拍 + `test_postgres_session_map_survives_restart` 重启续读)。
- llm-gateway:`uv run pytest` 104 passed。`test_token_passthrough_e2e.py`
  因依赖 `make proto-gen-py` 生成的 `cocola.*` stub(本地未生成)收集报错,
  属既有环境缺口,与 M7 无关,已 `--ignore` 隔离。
- Go(go 1.25 容器内,`GOWORK=off`):gateway / sandbox-manager / admin-api
  (store/httpapi/redispub)/ go-common(token)全部 `ok`。
- `ruff check`(agent-runtime / llm-gateway / py-common / scripts):All checks passed。
- `gofmt -l -s`(全部纳管 .go,排除 gen/*.pb.go):无输出,干净。

## 备注

- Web 端「重启后同一 session_id 续接对话(--resume 生效)」为手工验收项,已在
  plan 标注;其充分条件(沙盒 `~/.claude` 持久化)已在第五步落地。
- 本步随附 plan 文档第 6 步勾选与状态更新。
