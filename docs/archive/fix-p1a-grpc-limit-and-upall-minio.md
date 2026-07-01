# fix(p1a): 抬高 gRPC 消息上限 + up-all 起 MinIO(大文件上传收尾)

## 问题

前端上限抬到 32MiB、gateway 已按阈值分流后,传 19MB 文件仍现两处联调 bug:

1. **gRPC 4MiB 默认上限**:上传大文件后对话报
   `rpc error: code = ResourceExhausted desc = SERVER: Received message larger
   than max (19998730 vs. 4194304)`。附件字节要穿过多个 gRPC 跳:
   gateway→agent-runtime 的 `Query`、agent-runtime→sandbox-manager 的 `WriteFile`
   (即使是后端代 pull 的大文件,最终仍要把整份字节写进沙箱)。这些 server/client
   都没显式设 `MaxRecvMsgSize`/`max_receive_message_length`,全部继承 gRPC 默认
   4MiB(4194304)。`SERVER:` 前缀说明是接收端(agent-runtime)拒收。
2. **make up-all 从不起 MinIO**:`localhost:9001` 打不开控制台。`make up-all`
   跑的是 `scripts/run-stack.sh`(原生起服务,非 docker-compose),它既不拉起 MinIO
   也不导出 `COCOLA_MINIO_*`;只有 `docker-compose.dev.yml`/`full.yml` 里才定义了
   minio。于是控制台(:9001)和 S3(:9000)根本没起,gateway 落桶降级为纯内联。

## 改动

### 1. gRPC 消息上限可配置(默认 64MiB)

新增统一配置源 `COCOLA_GRPC_MAX_MESSAGE_BYTES`,非法/缺省回落 **64MiB**(高于
前端 32MiB 硬上限,给 base64/proto framing 留足余量),接线到全部 4 个 gRPC 面:

- `apps/agent-runtime/cocola_agent_runtime/grpc_limits.py`(新增):集中定义
  `DEFAULT_MAX_MESSAGE_BYTES=64MiB`、`max_message_bytes()`(读 env,非正/非法回落)、
  `channel_options()`(返回 send/receive 两个 option)。
- `apps/agent-runtime/cocola_agent_runtime/__main__.py`:`grpc.aio.server(...)`
  加 `options=channel_options()`,解 `SERVER:` 4MiB 拒收。
- `apps/agent-runtime/cocola_agent_runtime/sandbox_client.py`:`insecure_channel`
  加 `options=channel_options()`,覆盖 WriteFile 写入沙箱这跳(sandbox_binder 走
  SandboxClient,自动继承)。
- `apps/gateway/internal/agent/client.go`:新增 `defaultMaxMessageBytes=64MiB` +
  `maxMessageBytes()`;`Dial` 加 `grpc.WithDefaultCallOptions(MaxCallRecvMsgSize,
  MaxCallSendMsgSize)`。
- `apps/sandbox-manager/cmd/sandbox-manager/main.go`:同款 const+helper;
  `grpc.NewServer` 加 `grpc.MaxRecvMsgSize/MaxSendMsgSize`。

### 2. run-stack.sh 拉起 MinIO + 导出 COCOLA_MINIO_*

- `scripts/run-stack.sh`:dev-token 段前插入 MinIO 启动块——当
  `COCOLA_SKIP_MINIO != 1` 且有 docker 时,`docker compose -f
  deploy/docker-compose/docker-compose.dev.yml up -d minio minio-init`,等
  9000 端口就绪后导出 `COCOLA_MINIO_ENDPOINT`(默认 127.0.0.1:9000)、
  `COCOLA_MINIO_ACCESS_KEY`(cocola)、`COCOLA_MINIO_SECRET_KEY`(cocola_dev_pw)、
  `COCOLA_MINIO_BUCKET`(cocola)、`COCOLA_ATTACHMENT_INLINE_MAX_BYTES`(16MiB);
  失败或无 docker/`COCOLA_SKIP_MINIO=1` 时优雅跳过(降级纯内联并告警)。所有 export
  均用 `${VAR:-default}`,已有 env 优先。启动横幅新增一行打印控制台地址
  `http://127.0.0.1:9001`(cocola / cocola_dev_pw)。

## 校验

- gateway `go build ./...` + `go vet ./internal/agent/` 全绿。
- sandbox-manager `GOWORK=off go build/vet ./...` 全绿(该模块不在 go.work)。
- agent-runtime ruff 全绿;`tests/test_grpc_limits.py` 8 passed。
- `bash -n scripts/run-stack.sh` 语法 OK。

## 备注

- 两个尺寸限制别混:分流阈值 `COCOLA_ATTACHMENT_INLINE_MAX_BYTES`(16MiB,gateway
  决定小文件内联 / 大文件仅 oss_key 后端代 pull)与本次新增的 gRPC 传输上限
  `COCOLA_GRPC_MAX_MESSAGE_BYTES`(64MiB,纯传输层天花板)是两回事。
- 需重跑 `make up-all` 使改动生效;之后控制台在 http://127.0.0.1:9001。
