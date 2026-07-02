# 修复 sandbox-manager 启动竞争降级 + 跨 compose 僵尸容器

日期: 2026-07-02
Plan: docs/plan/fix-sandbox-manager-redis-retry-and-orphans.md

## 症状

`make up-all` 后 web 端对话报
`sandbox acquire failed: ... UNIMPLEMENTED "binder not configured"`;
sandbox-manager 日志:
`redis unavailable; session-binding RPCs disabled err="dial tcp: lookup redis ... no such host"`。

## 两个独立缺陷

1. **一锤子 ping,失败即永久降级**:`cmd/sandbox-manager/main.go` 启动时只
   `rds.New` ping 一次(5s),失败就把 binder 置空并永久降级,`Acquire`/`Heartbeat`/
   `Release` 全返回 `Unimplemented: binder not configured` 且不重连。哪怕 Redis 随后
   就绪也卡在降级态,直到手动重启。DNS/网络瞬时抖动即可触发。
2. **跨 compose 共用镜像 + restart:unless-stopped → 僵尸容器**:`docker-compose.yml`
   (项目 `cocola-minimal`)与 `docker-compose.full.yml`(项目 `cocola`)共用镜像
   `cocola/sandbox-manager:dev` 且都带 `restart: unless-stopped`。跑过 minimal 后其
   redis 被拆,那个 sandbox-manager 容器持续自动重启,挂在已无 `redis` 服务名的旧
   网络上,永远 `no such host`。本次报错正是此残留容器所致。

## 改动

### apps/sandbox-manager/cmd/sandbox-manager/main.go(Fix 1)
- 新增 `dialRedisWithRetry(ctx, log, budget)`:在总时长
  `COCOLA_REDIS_CONNECT_TIMEOUT`(默认 30s)内以 ~2s 间隔重试 `rds.New`,任一次成功
  即返回;穷尽仍失败才降级。每次重试打 info(第几次 / 剩余时间),最终失败保留原
  warn。
- 新增 `redisConnectTimeout()`:解析 `COCOLA_REDIS_CONNECT_TIMEOUT`,非法/非正回落 30s。
- 保留降级路径(本地无 Redis 单进程调试仍可跑),只是把"启动瞬时竞争"挡在重试窗口内。

### scripts/start.sh(Fix 2)
- full.yml 的 `up -d`(`up` 与 `--build` 两处)追加 `--remove-orphans`,启动时自动清
  掉本项目(name: cocola)命名空间下的孤儿容器。
- 说明:`--remove-orphans` 只清同一 compose 项目;跨项目的 `cocola-minimal` 残留需
  一次性手动清(见下),此后 full 栈自身不再累积孤儿。

## 验证

- sandbox-manager 容器内 `GOWORK=off go build/vet/test` 全绿(BUILD_OK / VET_OK /
  全包 ok)。
- `bash -n scripts/start.sh` 通过。
- 端到端(重建镜像 + up + web chat 不再报 binder not configured)由用户本地执行。

## 一次性清残留(跨项目僵尸容器)

```
docker compose -f deploy/docker-compose/docker-compose.yml -p cocola-minimal down --remove-orphans
```

## 影响

- 启动更鲁棒:Redis 晚就绪几秒不再导致永久降级。
- `make up-all` 每次启动自动清本项目孤儿容器,减少"僵尸挂旧网络"类问题复发。
