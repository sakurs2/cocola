# Fix: 拆栈/启动彻底清理动态沙箱容器,避免端口占用导致 make up 失败

## 背景
用户反馈两个相关问题:
1. `make up`(--hybrid 原生模式)启动失败,preflight 报 `:50051 is published
   by container 'sandbox-177b17ac-...'`,提示先跑 `bash scripts/start.sh --down`。
2. 但跑完 `bash scripts/start.sh --down` 后端口**仍未释放**,依旧被占。

## 根因
sandbox-manager 运行时经 OpenSandbox server **动态创建 per-session 沙箱容器**
(名字 `sandbox-<uuid>`、镜像 `cocola/sandbox-runtime:dev`、带 `opensandbox.io/id`
label)。这些容器**不属于任一 compose 文件**,因此:
- `scripts/start.sh --down/--stop` 只做 `compose down/stop` + `opensandbox_down`,
  reap 不到它们 → 端口(如 :50051)一直被占 → 死锁。
- `scripts/run-stack.sh` 的 hybrid preflight 检出该容器占用 :50051 后直接报错退出,
  而它建议的 `--down` 又清不掉 → 用户被卡死。

## 变更
1. **scripts/start.sh** —— 新增 `cleanup_sandboxes()`:按镜像
   `ancestor=cocola/sandbox-runtime:dev` 精确匹配并 `docker rm -f` 强删动态沙箱容器。
   接入 `--down`(在 `opensandbox_down` 之后)与 `--stop` 两条路径,确保拆栈后端口彻底释放。
2. **scripts/run-stack.sh** —— hybrid preflight 端口冲突检查**之前**增加自愈块:
   先按同一 ancestor filter 移除遗留的 per-session 沙箱容器(它们始终是临时会话沙箱、
   永不属于应用栈,reap 绝对安全),使"仅一个陈旧沙箱占端口"这种最常见场景不再阻塞
   `make up`。free_port 的安全约束不变——绝不杀容器引擎/代理进程。

## 不动
- 全容器应用栈的 reap 仍走 compose down(动态沙箱与应用容器职责分离)。
- docs/plan/*、docs/archive/* 历史文档保留原样。

## 校验
- `bash -n scripts/start.sh` / `bash -n scripts/run-stack.sh` 通过。
- 手动 `docker rm -f` 掉两个阻塞容器(sandbox-177b17ac-... / sandbox-a9ee4a2f-...)后
  :50051 已释放,make up preflight 不再报冲突。

## 回滚
`git revert` 本次提交即可。
