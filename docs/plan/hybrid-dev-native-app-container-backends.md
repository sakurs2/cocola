# 混合 dev 模式:容器后端 + 原生 app(真实 Route A,零镜像重建)

日期: 2026-07-02

## 背景 / 痛点

`make up-all` 收敛为全容器栈后,每改一次代码都要重构镜像才能验证,迭代很慢。
但"原生跑"其实一直都在:
- `make up` / `make up-web`(`scripts/run-stack.sh`)始终以原生前台进程跑
  gateway + agent-runtime(+ web),`go run` / `uv run` / `pnpm dev`,改完直接重启,
  不碰镜像。
- 唯一缺陷:原生模式下 `COCOLA_SANDBOX_ADDR` 为空 → agent-runtime 回落 **EchoProvider**
  (无真实模型、无真实 Route A)。于是"要验真实行为"就被逼到全容器 `up-all`,吃镜像重建。

关键事实(已核实):
1. agent-runtime 纯粹按 `COCOLA_SANDBOX_ADDR` 是否非空来选 provider —— 非空即真实
   Route A(`InSandboxShimProvider`),空即 Echo(`__main__.py` `_build_provider`)。
2. full.yml 的后端全都**发布了宿主端口**:redis 6379、postgres 5432、minio 9000、
   sandbox-manager 50051、llm-gateway 18091、admin-api 8092。
3. sandbox-manager 用 DooD(挂 `/var/run/docker.sock`)在宿主 daemon 上创建**兄弟容器**
   沙箱,沙箱经 `host.docker.internal:18091` 找 llm-gateway —— 这条不依赖 app 是否容器化。

结论:把"少改、构建贵"的后端(infra + sandbox-manager + llm-gateway + admin-api)留在
容器里,把"高频改"的 gateway / agent-runtime / web 原生跑,用宿主端口对接,即可得到
**真实 Route A + 真实模型**,且改这三者时**零镜像重建**。

## 目标

新增混合 dev 模式:一条命令起容器后端子集 + 原生 app,拿到真实 Route A。
不新增第二条"产品 route"(Route A 仍是唯一 agent 路径),只是把它的控制面进程原生跑。

## 方案

### scripts/run-stack.sh: 加 `--hybrid`
`--hybrid` 时:
1. 用 **full.yml(project `cocola`)** 起后端子集(而非 dev.yml,因为 sandbox-manager
   要靠服务名 `redis` 解析,必须在同一 compose 网络):
   `redis postgres minio minio-init sandbox-manager llm-gateway admin-api`。
   provider=opensandbox 时,复用 start.sh 的 opensandbox server 起法(宿主 :8090)。
   —— 实现上抽公共函数或直接调 `docker compose -f full.yml --env-file .env up -d <subset>`。
2. 等 sandbox-manager(:50051)、llm-gateway(:18091/healthz)、admin-api(:8092/healthz)就绪。
3. 导出原生 app 需要的宿主端口 env,再走**现有**的原生 agent-runtime + gateway(+ web)启动:
   - `COCOLA_SANDBOX_ADDR=127.0.0.1:50051`(→ 真实 Route A)
   - `COCOLA_SANDBOX_IMAGE=cocola/sandbox-runtime:dev`
   - `COCOLA_SANDBOX_LLM_BASE_URL=http://host.docker.internal:18091`(注入沙箱,沙箱在宿主桥上)
   - `COCOLA_SANDBOX_LLM_TOKEN=cocola-local`、`COCOLA_SANDBOX_MODEL_ALIAS=cocola-default`
   - `COCOLA_ADMIN_BASE_URL=http://127.0.0.1:8092`
   - `COCOLA_PG_DSN=postgres://cocola:cocola_dev_pw@127.0.0.1:5432/cocola?sslmode=disable`
   - `COCOLA_MINIO_ENDPOINT=127.0.0.1:9000`(+ access/secret/bucket)
   - `COCOLA_AUTH_ALLOW_ANON=1`(gateway 空 token 当 dev-user)
   - `COCOLA_SKIP_MINIO=1`(full.yml 已起 minio 于 :9000,别再拿 dev.yml minio 抢 9000)
4. 生命周期:Ctrl-C 只停原生进程(沿用现有 cleanup);容器后端留着,便于快速重启原生。
   停后端用 `bash scripts/start.sh --stop`(同 project `cocola`)或 `--down`。

### Makefile: 加 `up-hybrid`
- `up-hybrid`: `bash scripts/run-stack.sh --hybrid`(可加 `--with-web`)。
- 更新 up 系列头注释,三档说清:
  - `up` / `up-web` —— 纯原生 Echo,零依赖快调(无真实模型)。
  - `up-hybrid` —— 原生 app + 容器后端,真实 Route A,改 app 零镜像重建(**日常迭代推荐**)。
  - `up-all` —— 全容器 Route A,最接近生产,改代码需重构镜像。

## 端口对照

| 组件 | 混合模式跑法 | 地址 |
|---|---|---|
| redis / postgres / minio | 容器(full.yml) | 127.0.0.1:6379 / 5432 / 9000 |
| sandbox-manager | 容器(full.yml) | 127.0.0.1:50051 |
| llm-gateway | 容器(full.yml) | 宿主 :18091 → 容器 :8080 |
| admin-api | 容器(full.yml) | 宿主 :8092 → 容器 :8090 |
| gateway | **原生** go run | 127.0.0.1:8080 |
| agent-runtime | **原生** uv run | 127.0.0.1:50061 |
| web | **原生** pnpm dev | 127.0.0.1:3000 |

## 验证

- `bash -n scripts/run-stack.sh`;`make -n up-hybrid` 解析正确。
- 端到端(起混合栈 + web chat 真实回复、改 agent-runtime 重启即生效)由用户本地跑。

## 影响

- 保留并强化"原生跑"能力:日常改 app 用 `make up-hybrid`,真实 Route A + 零镜像重建。
- `make up` / `up-all` 语义不变;不新增产品 route。
