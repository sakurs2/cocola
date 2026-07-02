# make up-all 收敛为唯一的全容器 OpenSandbox 栈

日期: 2026-07-02

## 背景

历史上本地有两套并行入口,职责割裂:

| 入口 | 底座 | 服务 | sandbox 后端 | 定位 |
|---|---|---|---|---|
| `make up*` → `scripts/run-stack.sh` | 原生前台进程(仅 MinIO 容器) | agent-runtime / gateway / (llm) / (web) | **无** → EchoProvider 降级 | 快速本地调试 |
| `bash scripts/start.sh` → `docker-compose.full.yml` | 全容器 | 9 个服务全齐 | **有**,真 Route A | 完整栈 |

问题:`make up-all` 名字暗示"全都起来了",实际走 `run-stack.sh --all`,`COCOLA_SANDBOX_ADDR`
默认空 → agent-runtime 回落 EchoProvider,根本不碰沙箱;注释里 "real SDK path" 是
Route B 时代的遗留话术,早已不准确。

按"只有一条 route"的既定原则(不再新建 `up-route-a` 之类分叉目标),将 `make up-all`
收敛到已经成型的全容器路径 `start.sh` / `full.yml` 上,消灭割裂。

## 目标形态

`make up-all` = **OpenSandbox server(host :8090)+ full.yml 全栈(9 服务,provider=opensandbox)**,
一条命令、全程容器、Ctrl-C / `--down` 能把两个 compose 一起干净拆掉。

选择 OpenSandbox 后端(方案 B)而非 docker/DooD(方案 A)的理由:用户明确要求沙箱后端
走 OpenSandbox server;且 `.env` 已配好 `COCOLA_SANDBOX_PROVIDER=opensandbox` +
`COCOLA_OPENSANDBOX_URL=http://host.docker.internal:8090/v1`,拼图只差把 server 的
生命周期并进全栈启停。

## 关键约束

- OpenSandbox server **不在 full.yml 的 compose 网络里**,而是宿主发布的独立 compose
  (`docker-compose.opensandbox.yml`,host :8090)。full.yml 里 sandbox-manager 通过
  `host.docker.internal:8090/v1` 跨 host 网络桥接连它。因此必须编排**两个 compose**,
  不是一个全包。
- 两条已知坑必须继续规避(start.sh 已处理):必带 `--env-file .env`(否则 llm-gateway
  回落 fake echo);网关发布端口 18091(绕开宿主遗留 :8081 的 401)。
- 助手无法在此环境运行 docker / make(网络监听限制),最终 `make up-all` 的真实拉起由
  用户本地执行。

## 改动

### 1. `scripts/start.sh`:把 OpenSandbox server 并入启停生命周期

- 新增内部函数 `opensandbox_up` / `opensandbox_down`,封装
  `docker compose -f docker-compose.opensandbox.yml up -d` + 轮询 `:8090/health`
  (复用 Makefile `opensandbox-up` 已验证的等待逻辑)/ `down`。
- 仅当 `COCOLA_SANDBOX_PROVIDER=opensandbox`(读 .env / 环境)时才拉起 server;
  provider=docker 时跳过(DooD 不需要 server),保持 start.sh 对两种后端都可用。
- `up` / `--build`:先 `opensandbox_up`(若需),再 full.yml `up -d`。
- `--down`:full.yml `down` 后,再 `opensandbox_down`。`--stop`:同理停两边。
- server 端口用 `COCOLA_OPENSANDBOX_HOST_PORT`(默认 8090),与 opensandbox.yml 对齐。

### 2. `Makefile`:up-all 指向全容器路径 + 修注释

- `up-all` 目标从 `bash scripts/run-stack.sh --all` 改为 `bash scripts/start.sh`
  (start.sh 内部已按 provider 决定是否带 OpenSandbox server)。
- 更新 up 系列头部注释块:删掉 "real SDK path" 的 Route B 遗留描述,改为准确说明
  —— `up`/`up-web` 是原生 Echo 快调路径,`up-all` 是全容器 Route A(OpenSandbox)完整栈。
- **不新建** `up-route-a` 等分叉目标(遵循"只有一条 route")。
- `up` / `up-web`(原生 Echo 快调)保持不变;`opensandbox-up`/`-down`、`start.sh` 其他
  子命令保留供单独调试。

## 验证

- `bash -n scripts/start.sh` 语法检查。
- `make -n up-all` 确认目标解析到 start.sh。
- 端到端拉起 + chat 验收由用户本地执行(助手不能跑 docker/make)。

## 影响面

- `make up-all` 语义变更:从"原生 Echo"变为"全容器 OpenSandbox Route A",行为更符合
  命名与用户预期。
- `up`/`up-web` 不变,轻量调试路径保留。
