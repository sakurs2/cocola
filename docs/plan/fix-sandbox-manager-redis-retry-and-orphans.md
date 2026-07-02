# 修复 sandbox-manager 启动竞争 + 跨 compose 僵尸容器

日期: 2026-07-02

## 症状

web 端对话报 `sandbox acquire failed: ... UNIMPLEMENTED "binder not configured"`。
sandbox-manager 日志:
`redis unavailable; session-binding RPCs disabled err="dial tcp: lookup redis ... no such host"`

## 两个独立缺陷

### 缺陷 1:一锤子 ping,失败即永久降级(启动竞争)

`cmd/sandbox-manager/main.go` 启动时 `rds.New(ctx, rds.ConfigFromEnv())` 只 ping 一次
(5s 超时)。失败就把 binder 置空并永久降级 —— `Acquire`/`Heartbeat`/`Release` 全部
返回 `Unimplemented: binder not configured`,且**不会重连**。哪怕 Redis 随后就绪,进程
仍卡在降级态,直到手动重启。

full.yml 里虽有 `depends_on: redis: service_healthy`,但这只保证 Redis 健康后才启动
sandbox-manager,一旦 DNS/网络抖动或容器编排在别的 compose(见缺陷 2)下,单次 ping
就把整个 binding 能力永久关掉,鲁棒性差。

### 缺陷 2:跨 compose 共用镜像 + restart:unless-stopped → 僵尸容器

`docker-compose.yml`(项目名 `cocola-minimal`,被 `make demo-minimal`/`sandbox-run` 使用)
和 `docker-compose.full.yml`(项目名 `cocola`)**共用镜像 `cocola/sandbox-manager:dev`
且都带 `restart: unless-stopped`**。跑过 minimal 后其 redis 被拆,但那个 sandbox-manager
容器一直自动重启,挂在一个已无 `redis` 服务名的旧网络上,永远解析失败 `no such host`。
本次 `no such host`(而非 `connection refused`)正是这一残留容器所致。

## 修复

### Fix 1 — main.go:Redis 连接带重试等就绪

- 新增 `dialRedisWithRetry(ctx, timeout)`:在总时长(`COCOLA_REDIS_CONNECT_TIMEOUT`,
  默认 30s)内以固定间隔(~2s)重试 `rds.New`,任一次成功即返回;穷尽仍失败才降级。
- 保留降级路径(本地无 Redis 单进程调试仍可跑,只是 binding RPC 关闭),但把"启动瞬时
  竞争"这一类失败挡在重试窗口内。
- 日志:重试期打 info(第几次、剩余时间),最终失败打原有 warn。

### Fix 2 — start.sh:up/build 带 --remove-orphans

- full.yml 的 `up -d` / `--build` 追加 `--remove-orphans`,启动时自动清掉本项目命名空间
  下的孤儿容器。
- 说明:`--remove-orphans` 只清**同一 compose 项目**(name: cocola)的孤儿。跨项目的
  `cocola-minimal` 残留需一次性手动清(changelog / 回复里给命令),但此后 full 栈自身
  不再累积孤儿。

## 验证

- sandbox-manager:容器内 `GOWORK=off go build/vet/test`。
- `bash -n scripts/start.sh`。
- 端到端(重建镜像 + up + web chat 不再报 binder not configured)由用户本地跑。

## 影响

- 启动更鲁棒:Redis 晚就绪几秒不再导致永久降级。
- `make up-all` 每次启动自动清本项目孤儿容器,减少"僵尸挂旧网络"类问题复发。
