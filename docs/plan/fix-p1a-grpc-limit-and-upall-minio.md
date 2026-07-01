# Plan — 修复大附件上传报错(gRPC 4MiB 上限)+ `make up-all` 起 MinIO

## 背景 / 症状

联调 P1a 附件上传时,用户报告两个现象:

1. 上传约 19 MB 文件后对话报错:
   `⚠️ rpc error: code = ResourceExhausted desc = SERVER: Received message larger than max (19998730 vs. 4194304)`
2. `localhost:9001`(MinIO web 控制台)打不开。

## 根因(两个,互相关联)

### 根因 A — 所有承载附件字节的 gRPC hop 都用了默认 4 MiB 上限

`4194304 = 4 MiB` 正是 gRPC 的**默认单条消息上限**。附件字节要经过多个 gRPC hop:

- gateway(Go)→ agent-runtime(Py):`agent.Dial` 只带了 transport creds + tracing,**没有 `MaxCallSendMsgSize` / `MaxCallRecvMsgSize`**。
- agent-runtime server:`grpc.aio.server(interceptors=...)`,**没有 `options`**,继承默认 4 MiB 接收上限 → 这就是 `SERVER:` 前缀报错的来源。
- agent-runtime → sandbox-manager(WriteFile 写附件进沙箱):`grpc.insecure_channel(addr)` 无 options;sandbox-manager `grpc.NewServer(...)` 无 size option。

因此**任何超过 ~4 MB 的附件都会在 wire 上被拒**,即便 gateway 的 inline 分流阈值是 16 MiB、前端上限是 32 MiB——这两个都比 4 MiB 大,自相矛盾。这是 P1a 接线遗留的结构性 bug。

### 根因 B — `make up-all` 根本不起 MinIO

`make up-all` 实际调用 `scripts/run-stack.sh`,它用 `go run` / `uv run` / `pnpm dev` **原生**拉起 agent-runtime + gateway(+ 可选 llm-gateway/web),**从不启动 MinIO,也不导出任何 `COCOLA_MINIO_*`**。P1a Step5 只把 MinIO 接进了 `docker-compose.full.yml` 和 `docker-compose.dev.yml`,漏了 `up-all` 真正走的这条脚本路径。

后果:MinIO 控制台 `:9001` 没有进程在听(现象 2);gateway 的对象存储保持 dark;19 MB 文件走 gateway 的**优雅降级 → inline 投递**,直接撞上根因 A 的 4 MiB 天花板(现象 1)。两个现象同源。

## 方案

### Step 1 — gRPC 消息上限可配置化(默认 64 MiB)

新增统一 env `COCOLA_GRPC_MAX_MESSAGE_BYTES`(默认 `67108864` = 64 MiB,舒适覆盖 16 MiB inline 阈值与 32 MiB 前端上限,给 base64 膨胀留余量),四处接线:

- **agent-runtime server**(`__main__.py`):`grpc.aio.server(options=[("grpc.max_receive_message_length", N), ("grpc.max_send_message_length", N)])`。
- **gateway client**(`agent/client.go` `Dial`):`grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(N), grpc.MaxCallSendMsgSize(N))`。
- **sandbox_client**(`sandbox_client.py`):`grpc.insecure_channel(addr, options=[...])`。
- **sandbox-manager server**(`main.go`):`grpc.MaxRecvMsgSize(N), grpc.MaxSendMsgSize(N)`。

各语言各写一个小 helper 读该 env(非法/非正 → 回落 64 MiB),不硬编码。

> 注意:这是传输层安全上限,与 ADR-0017 的 16 MiB inline 分流阈值语义不同——后者决定「inline push vs 后端 pull」,前者只是「单条 gRPC 消息物理上限」。大文件走后端 pull 后 gateway→agent-runtime 那一跳不带字节;但沙箱 WriteFile 那一跳仍搬真实字节,所以这层上限对两条投递路径都必要。

### Step 2 — `run-stack.sh` 起 MinIO 并导出 `COCOLA_MINIO_*`

`up-all` 时:

- 若本机有 docker:`docker compose -f deploy/docker-compose/docker-compose.dev.yml up -d minio minio-init`(dev.yml 已定义,bucket=`cocola`,console `:9001`),`wait_port 9000`。
- 导出 agent-runtime + gateway 需要的 env(默认对齐 dev.yml):
  `COCOLA_MINIO_ENDPOINT=127.0.0.1:9000`、`COCOLA_MINIO_ACCESS_KEY=cocola`、`COCOLA_MINIO_SECRET_KEY=cocola_dev_pw`、`COCOLA_MINIO_BUCKET=cocola`、`COCOLA_ATTACHMENT_INLINE_MAX_BYTES=16777216`。已存在 env 覆盖默认。
- ready banner 打印 MinIO 控制台 `http://127.0.0.1:9001`。
- 优雅降级:docker 不可用或 `COCOLA_SKIP_MINIO=1` → 跳过并告警,gateway 退回 P0 inline-only。

### Step 3 — 校验 + changelog + 提交

- Go:各模块 `go build ./...` + `go test ./...`。
- Py:`.venv/bin/ruff check` + `.venv/bin/pytest`。
- Web:`tsc --noEmit`(本次不改 web,lint 一次确认无回归)。
- `docs/archive/fix-p1a-grpc-limit-and-upall-minio.md` changelog;提交。

## 验收

用户重跑 `make up-all` 后:
1. `:9001` MinIO 控制台可打开(cocola / cocola_dev_pw)。
2. 上传 19 MB(>16 MiB 阈值)→ 走后端 pull,gateway→agent-runtime 不搬字节,沙箱 WriteFile 搬 19 MB < 64 MiB,通过。
3. 上传 5–16 MB → 走 inline push,< 64 MiB gRPC 上限,通过(旧版会在 4 MiB 挂掉)。
