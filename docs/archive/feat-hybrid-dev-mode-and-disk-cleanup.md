# feat: 混合 dev 模式(容器后端 + 原生 app)+ 无用镜像/卷清理

## 背景 / 动机

全栈容器化(`make up-all`)给出了真实 Route A + 真实模型,但每改一次
`agent-runtime` / `gateway` 代码都要重建镜像才能看到效果,内环反馈太慢。

原生栈(`make up` / `up-web`)改代码即时生效,但没有 `sandbox-manager` /
`llm-gateway`,`COCOLA_SANDBOX_ADDR` 为空 → 落到 `EchoProvider`(纯回显,无模型)。

用户诉求:"每次改代码都要重新构建镜像很麻烦,可不可以保留原生跑的能力"。
本次新增一个 **混合 dev 模式**,把两档之间的鸿沟补上——注意这不是新的产品
route,仍是唯一的 Route A,只是它的一种「跑法」。

## 变更一:混合 dev 模式(`make up-hybrid` / `run-stack.sh --hybrid`)

后端容器化、app 原生,得到真实 Route A + 真实模型,且改 app 代码零重建镜像。

- `scripts/run-stack.sh`
  - 新增 `HYBRID` 变量与 `--hybrid` 旗标(顺带置 `WITH_WEB=1`,默认带上原生
    web 前端 `:3000`,`pnpm dev` 改前端代码热更新),以及用法头注释。
  - 在 setsid 块之后插入 `hybrid_up()`:
    - 校验 docker 守护进程可用;
    - 若 provider 为 `opensandbox`(env 优先,其次读 `.env`)则先拉起独立
      OpenSandbox server(宿主 `:8090`)并轮询 `/health`;docker/DooD 后端跳过;
    - `docker compose -f docker-compose.full.yml [--env-file .env] up -d` 只起
      **后端子集**:`redis postgres minio minio-init sandbox-manager
      llm-gateway admin-api`(幂等 `up -d`,已在跑的容器不动);
    - `wait_port` 等 6379/5432/9000/50051/`${COCOLA_LLM_HOST_PORT:-18091}` /
      `${COCOLA_ADMIN_HOST_PORT:-8092}` 就绪;
    - 导出宿主发布端口环境,供随后原生启动的 app 接入:
      `COCOLA_SANDBOX_ADDR=127.0.0.1:50051`(**非空 → 走 InSandboxShimProvider,
      即真 Route A**)、`COCOLA_SANDBOX_IMAGE`、
      `COCOLA_SANDBOX_LLM_BASE_URL=http://host.docker.internal:<llm端口>`
      (沙箱是 DooD 兄弟容器,经宿主桥接回连网关)、`COCOLA_SANDBOX_LLM_TOKEN`、
      `COCOLA_SANDBOX_MODEL_ALIAS`、`COCOLA_ADMIN_BASE_URL`、`COCOLA_PG_DSN`、
      `COCOLA_MINIO_*`、`COCOLA_AUTH_ALLOW_ANON=1`、`COCOLA_SKIP_MINIO=1`
      (MinIO 已容器化,跳过 run-stack 自带的 dev.yml MinIO);
      所有导出均 `${VAR:-default}`,一次性 override 仍生效。
  - ready banner 增加两行:后端清单 + "停后端用 `start.sh --stop`"。
  - 后端不随 Ctrl-C 拆除(这正是快内环的意义):Ctrl-C 只重启原生 app,
    热后端存活;停后端用 `bash scripts/start.sh --stop` / `--down`(同 `cocola` 工程)。
- `Makefile`
  - 新增 `up-hybrid` 目标 → `bash scripts/run-stack.sh --hybrid`,并入 `.PHONY`。
  - dev-stack 头注释由「两档」改写为「三档」:`up`(原生 Echo,无模型)/
    `up-hybrid`(容器后端 + 原生 app,真 Route A,改代码免重建)/ `up-all`(全容器)。

### 三档速览

| 目标 | app | 后端 | 模型 | 改 app 代码 |
|---|---|---|---|---|
| `make up` / `up-web` | 原生前台 | 仅 MinIO | 无(EchoProvider) | 即时生效 |
| `make up-hybrid` | 原生前台(含 web :3000) | 容器化子集 | 真实 | **免重建镜像**,重启即生效 |
| `make up-all` | 全容器 | 全容器 | 真实 | 需重建镜像 |

## 变更二:无用镜像/容器/卷清理

用户诉求:"现在镜像和容器占用了太多磁盘空间,请清理掉无用"。在不打断正在
运行的 10 容器栈的前提下回收 ~2GB:

- 删 4 个停止的容器(~32MB)、`golang:1.24`(911MB)、悬空(dangling)镜像
  (1.094GB)、19 个孤儿 session 卷。
- **保留**:`cocola/sandbox-runtime:dev`(7.9GB,`FROM opensandbox/
  code-interpreter:v1.1.0` 7GB,删基础镜像不回收空间且断构建)、
  `cocola-gomod25`(容器化 sandbox-manager 构建的 Go modcache,瞬时挂载显示 0
  链接,不可 prune)、全部数据卷(redisdata/pgdata/miniodata)与在用 builder 镜像。
- 此清理为一次性运维动作,不落脚本;记录于此备查。

## 验证

- `bash -n scripts/run-stack.sh` 通过。
- `bash scripts/run-stack.sh --help` 正确渲染 `--hybrid` 段落。
- 未知旗标仍以 exit 2 拒绝(`--bogus`)。
- `make -n up-hybrid` → `bash scripts/run-stack.sh --hybrid`。
- 磁盘清理后原 10 容器栈 `docker compose ps` 健康未受影响。

端到端(`make up-hybrid` 起栈 + Web/curl 对话真实模型)由使用者本机验收。

## 相关

- ADR-0009(Route A,brain-in-sandbox,唯一产品路径)
- docs/plan/hybrid-dev-native-app-container-backends.md
