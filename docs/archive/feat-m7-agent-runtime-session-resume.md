# feat(m7): agent-runtime session_map 落 Postgres + 沙盒 ~/.claude 持久化

M7 持久化第五步:让 Route A 的 `--resume` 续接在 agent-runtime 重启后仍然成立。
两件事缺一不可:

1. **充分条件**——会话的真身是沙盒内 `~/.claude/projects/<proj>/<uuid>.jsonl`
   这份磁盘文件。本步把它落到 sandbox-manager 的 per-user 宿主目录,使其在沙盒
   重建后依然存在。
2. **索引**——`session_map` 表只是 cocola `session_id -> claude_session_id` 的
   索引,告诉下一轮该 `resume` 哪个 claude 会话;它本身不是会话状态。原先这层
   索引是 `InSandboxShimProvider` 进程内的一个 dict,重启即丢,本步落到 Postgres。

未配置 `COCOLA_PG_DSN` 时退化为进程内索引,保零依赖 dev 启动。

## 改动

- `apps/agent-runtime/cocola_agent_runtime/session_map.py`(新增):
  `SessionMap` Protocol(`get`/`put`/`aclose`)+ 两实现。`MemorySessionMap`
  进程内 dict(单进程生命周期内可续接);`PostgresSessionMap` 写 `session_map`
  表(`INSERT ... ON CONFLICT (session_id) DO UPDATE`,以最新 claude_session_id
  为准;空值是 no-op,不覆盖已有绑定)。连接池 `AsyncConnectionPool` 懒打开,
  同步组合根可在无事件循环时构造。schema 归 `db/migrations`,本模块只读写。
- `apps/agent-runtime/cocola_agent_runtime/shim_provider.py`:`_session_resume`
  进程内 dict 换成注入的 `SessionMap`(默认 `MemorySessionMap`)。`query` 开头
  从索引异步取 `resume`;turn 结束后尽力 `put`(索引写失败不影响本轮)。
- `apps/agent-runtime/cocola_agent_runtime/__main__.py`:新增 `_build_session_map`,
  `COCOLA_PG_DSN` 有值 -> `PostgresSessionMap`,否则 `MemorySessionMap`;
  Route A 构造 provider 时注入。补充 env 文档。
- `apps/agent-runtime/pyproject.toml`:新增 `psycopg[binary]>=3.1`、
  `psycopg-pool>=3.2`(uv 管理)。
- `apps/sandbox-manager/internal/provider/docker/docker.go`:为每个 Route A 沙盒
  bind-mount per-user `<root>/claude/<user_id>` -> `/home/cocola/.claude`
  (`CLAUDE_CONFIG_DIR`),并 chown 到沙盒非 root uid(10001),使沙盒内 claude CLI
  能落盘会话文件。这才是 `--resume` 跨沙盒重建存活的充分条件(ADR-0008 T2,跨会话)。
  此前 Dockerfile 注释声称 `~/.claude` 已挂到 per-user volume,实际 provider 未挂,
  本步补齐。
- `deploy/docker-compose/docker-compose.full.yml`:在 sandbox-manager 的
  `COCOLA_DATA_ROOT` 卷注释里说明 `<root>/claude/<user_id>` 即沙盒
  `~/.claude` 的持久化来源,并点明 session_map 仅为索引。

## 设计取舍

- **session_map = 索引,磁盘文件 = 充分条件**:Claude Agent SDK 的 `--resume`
  依赖磁盘上的 `~/.claude` jsonl,光有 id 无文件无法复活会话。因此持久化重心放在
  沙盒卷,而非把会话状态塞进 PG/Redis(与 Mira「序列化进 Redis、任意 Pod 续接」
  的模型有意分道)。
- **沙盒级而非命令级**:`~/.claude` 落 per-user 目录,与既有 session 级沙盒收敛
  (`sandbox_binder`)一致,不回退到命令级。

## 测试

- `apps/agent-runtime/tests/test_session_map.py`(新增):Memory 与 Postgres 跑
  同一 `SessionMap` 契约;PG 腿由 `COCOLA_TEST_PG_DSN` 门控,未设即 skip。含
  `test_postgres_session_map_survives_restart`——新建 store 实例(模拟重启)仍能
  从持久表读回绑定。
- `uv run pytest`(agent-runtime 全量):48 passed, 2 skipped(PG 门控)。
- PG 对拍腿:对真实 postgres:16-alpine(应用 db/migrations schema v1)实跑,3 passed。
- `gofmt -l` 对 docker.go 干净;ruff check / format 对新增/改动 Python 文件全绿。
- 注:sandbox-manager 的 `go build` 需 go 1.25 工具链,本地离线环境不可下载;
  仅以 gofmt 校验语法,构建留待 CI。
