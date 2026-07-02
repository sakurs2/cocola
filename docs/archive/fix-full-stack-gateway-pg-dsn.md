# Fix: 全容器栈 gateway 缺 COCOLA_PG_DSN(对话持久化在 up-container 下不生效)

## 症状
`make up`(原生模式)对话能持久化、侧边栏能列历史;但 `make up-container`
(全容器 full.yml)下侧边栏始终为空、刷新后对话丢失——持久化没启用。

## 根因
对话持久化(route A)的写路径在 **gateway**。`docker-compose.full.yml` 给
llm-gateway / admin-api / agent-runtime 都注入了 `COCOLA_PG_DSN`(M7 session_map
时期接的),唯独 **gateway 服务块没有**。gateway 启动时 `COCOLA_PG_DSN` 为空
→ 持久化特性暗置(feature dark),聊天照常但一行都不落库。

原生模式无此问题:`run-stack.sh` 的 hybrid_up 里全局 `export COCOLA_PG_DSN`,
native gateway 子进程继承得到。

## 改动
- `deploy/docker-compose/docker-compose.full.yml` gateway 服务:
  - `environment` 增 `COCOLA_PG_DSN`,默认指向本栈 postgres 节点
    (`postgres://cocola:cocola_dev_pw@postgres:5432/cocola?sslmode=disable`,
    与其它服务同值,可被 repo-root .env 覆盖)。
  - `depends_on` 增 `postgres: condition: service_healthy`,保证 gateway 起来时
    库已就绪(goose 迁移幂等,谁先起谁建表)。

## 验证
- `docker compose -f ...full.yml config` 通过;gateway 解析后
  `depends_on=[agent-runtime, minio-init, postgres]`、
  `COCOLA_PG_DSN=postgres://cocola:cocola_dev_pw@postgres:5432/cocola?sslmode=disable`。
- `start.sh`(封装 full.yml)无需改动:默认 DSN 已在 compose 内生效。

## 关联
- 前置: feat-conversation-persistence-history-rendering.md(d9c72e1)
- Plan: docs/plan/conversation-persistence-history-rendering.md
