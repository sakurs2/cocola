# Changelog: 单一原生调试模式(仅沙箱依赖容器化,其余全原生)

日期:2026-07-02

## 动机

此前三档 dev 启动(纯原生 Echo / hybrid 容器后端 / 全容器)让调试路径分散。
明确唯一诉求:**除沙箱本身依赖的容器能力外,所有 cocola 服务原生运行**,专注这一个模式调试。

之前的 `--hybrid` 仍把 `sandbox-manager` / `llm-gateway` / `admin-api` 容器化,与该诉求冲突——
改任一服务代码仍需重建镜像。

## 变更

**原地把 `--hybrid` / `make up-hybrid` 重定义为唯一调试模式**(不新增目标):

- **容器仅保留沙箱依赖**:
  - OpenSandbox server(host `:8090`,provider=opensandbox 时);
  - redis / postgres / minio(`docker-compose.dev.yml`)。
  - 不再容器化 sandbox-manager / llm-gateway / admin-api。
- **所有 cocola 服务原生前台运行**:
  - `sandbox-manager` `:50051`——`cd apps/sandbox-manager && GOWORK=off go run ./cmd/sandbox-manager`
    (独立于 go.work 的 module,必须 `GOWORK=off`);
    `COCOLA_SANDBOX_PROVIDER=opensandbox`,
    **强制** `COCOLA_OPENSANDBOX_URL=http://127.0.0.1:8090/v1`
    (覆盖 `.env` 里的 `host.docker.internal`,后者仅容器内可解析);
    保持 server-proxy exec(不设 `COCOLA_OPENSANDBOX_DIRECT_EXEC`)。
  - `llm-gateway` `:8081`——hybrid 下自动开启(`--hybrid` 隐含 `WITH_LLM=1`);
    auth 关闭(`COCOLA_AUTH_SECRET=""`,与全容器栈一致,使沙箱大脑 dev token 通过);
    redis 指向 `127.0.0.1:6379`。
  - `admin-api` `:8092`——新增原生启动块;监听 `:8092` 而非 `:8090`(避开 OpenSandbox server);
    agent-runtime `COCOLA_ADMIN_BASE_URL=http://127.0.0.1:8092`(软依赖)。
  - `agent-runtime` `:50061`、`gateway` `:8080`、`web` `:3000` 沿用原生启动。
- **接线**:agent-runtime `COCOLA_SANDBOX_ADDR=127.0.0.1:50051` → 真 Route A(InSandboxShimProvider);
  注入沙箱的 `COCOLA_SANDBOX_LLM_BASE_URL=http://host.docker.internal:8081`
  (大脑在沙箱容器内,经宿主网桥回连原生 llm-gateway)。
- **生命周期**:沙箱/基建容器不随 `Ctrl-C` 拆(快内循环);原生进程随 `Ctrl-C` 全部释放
  (`OWNED_PORTS` 追加 50051 / 8092)。停容器:`make dev-down` + `make opensandbox-down`。

## 涉及文件

- `scripts/run-stack.sh`:重写 `hybrid_up()`(只起沙箱+基建);新增原生 sandbox-manager 启动;
  `--hybrid` 隐含 `WITH_LLM=1`;llm-gateway 在 hybrid 下 auth-off + redis 接线;新增原生 admin-api 块;
  banner 改述;`OWNED_PORTS` 追加 50051/8092。
- `Makefile`:`up-hybrid` help 与 dev-stack 头部注释改述为"唯一调试模式:仅沙箱+基建容器化,其余全原生"。
- `docs/plan/single-native-debug-mode.md`:Plan。

## 校验

- `bash -n scripts/run-stack.sh` 通过。
- `cd apps/sandbox-manager && GOWORK=off go build ./cmd/sandbox-manager` → OK。
- `go build ./apps/admin-api/cmd/admin-api`(workspace)→ OK。
- 端到端起栈与真模型对话由用户在本机验收。

## 保留说明

`make up`(纯原生 Echo,无模型,最快内循环)与 `make up-all`(全容器,贴近生产的整栈验收)保留;
`up-hybrid` 是日常真 Route A 调试的唯一模式。
