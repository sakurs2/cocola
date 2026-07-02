# Plan: 单一原生调试模式(仅沙箱依赖 + 基建容器化,其余全原生)

## 背景与目标

历史上本仓库存在三档 dev 启动方式:`make up`/`up-web`(纯原生 EchoProvider,无模型)、
`make up-hybrid`(容器化后端子集 + 原生 app)、`make up-all`(全容器 Route A)。

用户最新、也是唯一的调试诉求:**除了沙箱本身依赖的容器能力,其它一切原生跑**,
不再维护/暗示其它模式,专注这一个模式进行调试。

"沙箱本身依赖的容器能力"包含两类必须容器化的东西:
1. **OpenSandbox server**(host `:8090`)——沙箱运行时本体,通过 docker.sock 以兄弟容器方式拉起 execd/egress;它天然必须在容器里。
2. **基础设施**:redis / postgres / minio —— 第三方有状态组件,原生装反而更折腾,继续容器化。

**所有 cocola 自研服务全部原生运行**,包括此前混合模式仍容器化的
`sandbox-manager` 与 `llm-gateway`(以及 `admin-api`),从而做到:改任意 cocola 代码后
`Ctrl-C` → 重跑即可,零镜像重建;同时是**真 Route A + 真模型**。

> 只有一个产品 route(Route A,ADR-0009)。本模式是该 route 的一种**本地运行变体**,
> 不是新增 route,也不新增 `up-route-a` 之类的目标。

## 目标形态(唯一模式)

```
容器(仅沙箱依赖):
  cocola-opensandbox  : OpenSandbox server        host :8090   (docker.sock, 拉 execd/egress 兄弟容器)
  cocola-dev          : redis :6379 / postgres :5432 / minio :9000,:9001  (docker-compose.dev.yml)

原生进程(前台,Ctrl-C 全部拉下来):
  sandbox-manager  :50051  (gRPC)  provider=opensandbox → 127.0.0.1:8090/v1
  llm-gateway      :8081          真模型上游(.env: anthropic/aiberm)
  admin-api        :8092          市场技能(agent-runtime 软依赖)
  agent-runtime    :50061  (gRPC) COCOLA_SANDBOX_ADDR=127.0.0.1:50051 → InSandboxShimProvider(真 Route A)
  gateway          :8080   (BFF)
  web              :3000   (Next.js)
```

## 关键接线与坑(逐条已核实)

1. **sandbox-manager 原生构建**:它是 go.work 之外的独立 module,必须
   `cd apps/sandbox-manager && GOWORK=off go run ./cmd/sandbox-manager`。从仓库根 `go build ./apps/sandbox-manager/...` 会报 "not one of the workspace modules"。
   已验证 `GOWORK=off` 在 module 目录内 BUILD_OK。

2. **`COCOLA_OPENSANDBOX_URL` 必须改写**:`.env` 里是
   `http://host.docker.internal:8090/v1`,`host.docker.internal` 只在**容器内**可解析。
   sandbox-manager 现在原生跑,必须改成 `http://127.0.0.1:8090/v1`。
   本模式脚本对 sandbox-manager 子进程**强制导出** `COCOLA_OPENSANDBOX_URL=http://127.0.0.1:8090/v1`
   (覆盖 .env)。

3. **保持 server-proxy(不要 DIRECT_EXEC)**:opensandbox provider 默认 `useServerProxy=true`
   (`!envtruthy(COCOLA_OPENSANDBOX_DIRECT_EXEC)`),向 server 索要可被任意客户端访问的代理 exec URL。
   若开 DIRECT_EXEC,server 会返回 `host.docker.internal` 的直连地址——原生 sandbox-manager 无法解析。
   故**不设** `COCOLA_OPENSANDBOX_DIRECT_EXEC`。

4. **redis 地址**:sandbox-manager 通过 `packages/go-common/redis` 读 `COCOLA_REDIS_ADDR`(默认 `localhost:6379`)。
   容器化 redis 已发布到 host `:6379`,原生进程默认即可命中;显式导出 `COCOLA_REDIS_ADDR=127.0.0.1:6379` 更稳。
   redis 不可达时 sandbox-manager 优雅降级(绑定类 RPC Unimplemented),不阻塞主链路。

5. **端口冲突:admin-api vs OpenSandbox server**。admin-api 原生默认 `COCOLA_ADMIN_ADDR=:8090`,
   与 OpenSandbox server 的 host `:8090` 冲突。原生 admin-api 必须改监听
   `COCOLA_ADMIN_ADDR=:8092`,agent-runtime 走 `COCOLA_ADMIN_BASE_URL=http://127.0.0.1:8092`。
   (admin-api 是 agent-runtime 的**软依赖**:不通只告警 "no market skills",不影响对话。)

6. **llm-gateway 原生 + auth**:全容器栈里 llm-gateway 刻意**不**注入 `COCOLA_AUTH_SECRET`
   (auth 关闭,dev 匿名 token 流才成立)。原生启动同样必须让 llm-gateway 的 `COCOLA_AUTH_SECRET` 为空,
   否则沙箱大脑携带的 `cocola-local` token 会被拒。llm-gateway 监听 `:8081`(`COCOLA_LLM_PORT`),
   避开 gateway BFF 的 `:8080`。redis 用 `COCOLA_LLM_REDIS_URL=redis://127.0.0.1:6379/0`,
   持久化 `COCOLA_PG_DSN` 指向 127.0.0.1:5432。

7. **沙箱大脑 → 原生 llm-gateway 的回连地址**:大脑跑在沙箱容器内,访问宿主要走
   `host.docker.internal`。故注入沙箱的 `COCOLA_SANDBOX_LLM_BASE_URL=http://host.docker.internal:8081`
   (host.docker.internal 在沙箱容器内可解析,指向宿主上的原生 llm-gateway)。
   `COCOLA_SANDBOX_LLM_TOKEN` 用 run-stack 铸的 dev token。

8. **agent-runtime provider 选择**:`COCOLA_SANDBOX_ADDR=127.0.0.1:50051` 非空 →
   `InSandboxShimProvider`(真 Route A);为空则 EchoProvider。本模式必置非空。

## 落地方案:把 `--hybrid` 重定义为这唯一模式

用户明确"不要再扯其它模式"。当前 `--hybrid`/`up-hybrid` 已是"容器后端+原生 app",
但它仍**容器化了 sandbox-manager/llm-gateway/admin-api**,与新诉求冲突。

决策:**原地重定义 `hybrid_up()`**,不新增目标。改动如下——

- `hybrid_up()` 不再 `docker compose ... up sandbox-manager llm-gateway admin-api`;
  改为只拉起:
  - OpenSandbox server(provider==opensandbox 时,复用 opensandbox compose,已有逻辑保留);
  - `docker-compose.dev.yml` 的 redis/postgres/minio/minio-init(基建)。
- 新增原生 **sandbox-manager** 子进程:
  `cd apps/sandbox-manager && GOWORK=off COCOLA_SANDBOX_PROVIDER=opensandbox
   COCOLA_OPENSANDBOX_URL=http://127.0.0.1:8090/v1 COCOLA_SANDBOX_ADDR=:50051
   COCOLA_REDIS_ADDR=127.0.0.1:6379 $SETSID go run ./cmd/sandbox-manager`
  (`GOWORK=off` 是硬约束);`wait_port 127.0.0.1 50051`。
- `WITH_LLM` 在 hybrid 下强制置 1,复用已有的原生 llm-gateway 启动块(在 `:8081`,
  auth 关闭)。
- 新增原生 **admin-api** 子进程,监听 `:8092`,导出 `COCOLA_ADMIN_BASE_URL=http://127.0.0.1:8092`。
- 导出给 agent-runtime/gateway 的接线改为**全原生地址**:
  `COCOLA_SANDBOX_ADDR=127.0.0.1:50051`、`COCOLA_SANDBOX_LLM_BASE_URL=http://host.docker.internal:8081`、
  `COCOLA_ADMIN_BASE_URL=http://127.0.0.1:8092`、`COCOLA_PG_DSN=…127.0.0.1:5432…`、
  `COCOLA_MINIO_ENDPOINT=127.0.0.1:9000`、`COCOLA_AUTH_ALLOW_ANON=1`、`COCOLA_SKIP_MINIO=1`
  (minio 已由 dev.yml 起)。
- 拆栈:基建/沙箱容器**不随 Ctrl-C 拆**(快内循环);原生进程随 Ctrl-C 全部释放(已有 cleanup + 端口兜底)。
  `OWNED_PORTS` 追加 50051 / 8092(以及 llm 的 8081、web 的 3000)。
- 停容器:`make dev-down`(基建)+ `make opensandbox-down`(沙箱)。

### 顺序(hybrid_up 内)
1. 校验 docker 可用。
2. provider==opensandbox → 起 OpenSandbox server,轮询 `/health`。
3. 起 dev.yml 基建(redis/postgres/minio/minio-init),`wait_port` 6379/5432/9000。
4. 起原生 sandbox-manager(GOWORK=off,provider=opensandbox,URL→127.0.0.1),`wait_port` 50051。
5. 导出全原生接线 env,`return`。
6. 主流程继续:llm-gateway(8081)→ admin-api(8092)→ token → agent-runtime(50061)→ gateway(8080)→ web(3000)。

> 注:admin-api 原生启动块需**新增**(此前脚本没有 admin-api 原生启动);放在 llm-gateway 之后、token 之前。

## Makefile / 文档

- `up-hybrid` help 文案改写为"唯一调试模式:仅沙箱(:8090)+基建容器化,其余全原生真 Route A"。
- dev-stack 头部注释:保留 `up`(纯原生 Echo,无模型,最快内循环)与 `up-hybrid`(唯一真 Route A 调试模式);
  `up-all`(全容器)保留用于"贴近生产的整栈验收",但明确它不是日常调试模式。
  (不删 up-all——它是发布前整栈验收入口,与"日常调试"正交;删掉会丢失能力。若用户要求彻底只留一个,再删。)

## 验收(交给用户端到端跑)

1. `make dev-down; make opensandbox-down`(清干净),`docker ps` 无 cocola 容器。
2. `make up-hybrid`:
   - 容器只应有 `cocola-opensandbox-server` + `cocola-dev-*`(redis/postgres/minio);
     **不应**出现 `cocola-sandbox-manager-1`/`cocola-llm-gateway-1`/`cocola-admin-api-1` 容器。
   - `.run-logs/sandbox-manager.log` 出现 opensandbox provider 就绪;`agent-runtime.log` 打印
     "using InSandboxShimProvider (Route A: brain in sandbox)"。
3. web `:3000` 发一条消息 → 真模型应答(经沙箱大脑 → host.docker.internal:8081 → llm-gateway → aiberm)。
4. 改一行 gateway/agent-runtime 代码 → Ctrl-C → `make up-hybrid` → 秒级重启,无镜像重建;容器原样复用。

## 校验手段(assistant 侧可做)
- `bash -n scripts/run-stack.sh` 语法;有 shellcheck 则跑。
- `cd apps/sandbox-manager && GOWORK=off go build ./cmd/sandbox-manager` 确认原生可构建(已验证)。
- 不实际起栈(端到端由用户跑)。

## 提交
- 单 commit;附 `docs/archive/` changelog。
