# Changelog:修复 `make up-hybrid` 误杀 OrbStack 引擎

日期:2026-07-02

## 症状

用户确认:**只要执行 `make up-hybrid`,OrbStack 就会自动退出**——
整个容器 VM 被关掉,所有容器(OpenSandbox server / redis / postgres / minio)一起死。
OrbStack `vmgr` 日志里看到 `"Received signal, requesting stop" reason=0` 后 VM 干净停机、随后冷启动。

## 根因

`scripts/run-stack.sh` 的 `free_port()` 在绑定每个端口前先“清路”:
`lsof -ti TCP:$port -sTCP:LISTEN` 拿到 PID,然后 `kill`。

在 OrbStack / Docker Desktop 上,**一个 PUBLISH 了宿主端口的容器,其宿主侧监听是由容器引擎自己的
单个代理进程**(OrbStack 里就是一个叫 `OrbStack` / `OrbStack Helper` 的进程,Docker Desktop 上是
`com.docker.backend` / `vpnkit`)**统一持有的**。因此对这类“容器发布端口”做 `lsof` 拿回来的
是那个引擎 PID——`kill` 它等于给整个引擎发 SIGTERM,VM 与全部容器随之被拆掉。

实测:宿主上 3000 / 8080 / 50051 / 50061 / 8090 / 5432 / 6379 / 9000 / 8092 全部由**同一个** `OrbStack`
进程监听。`up-hybrid` 在全容器栈仍持有 50051/8080/8092 时跑,`free_port` 把 `OrbStack` 杀了,VM 弹跳。

`vmgr` 里的 “Received signal, requesting stop” 不是外部信号,是**我们自己**通过 `free_port` 发出的 kill。

## 修复

### 1. `free_port()` 改为故障安全(绝不杀引擎/代理/未知进程)

- 新增 `_engine_name_re`,匹配 `OrbStack|com.docker|docker|vpnkit|qemu|colima|lima|containerd|dockerd`。
- 新增 `_reapable_pids_on_port()`:用 `lsof -Fpc`(**不是 `ps`**)取每个监听进程的命令名,
  只回收**能明确判定为我方原生子进程**(go run / uv run / pnpm dev 等)的 PID;
  任何命中引擎正则、或**无法正向判定**的进程一律**跳过**。
  - 为什么不用 `ps`:锁定的企业 macOS 上 `ps` 可能被拒(`Operation not permitted`),
    输出为空会“掉进” kill 分支;`lsof -Fpc` 在同环境下仍能稳定输出命令名。
- `free_port()`:若某端口无“可回收”PID(即端口被容器引擎/未知进程持有),
  **打印提示并原样保留**,引导用户用 `docker` / `make dev-down` / `make opensandbox-down` 停对应容器。
- 原则:**判不准就不杀**(fail-safe),彻底杜绝误伤引擎。

### 2. `hybrid_up()` 增加“全容器栈冲突”预检

`--hybrid` 会把 sandbox-manager / gateway / admin-api / agent-runtime / web **原生**拉起,
这些宿主端口必须空闲。若旧的全容器栈仍在跑,其容器会 PUBLISH 同名端口——原生绑定必失败,
而这些端口又是引擎代理持有(现在不会也不该去回收)。因此在 `docker info` 预检后新增:
用 `docker ps --filter publish=<port>` 探测 50051/8080/8092/3000/8081 是否被容器占用,
命中则**提前失败并提示 `make dev-down`**,而不是启动到一半再崩。

## 影响

- `make up-hybrid` 不再杀死 OrbStack。
- 与全容器栈端口冲突时,启动前即给出明确指引,不再半路失败。
- 仅改 `scripts/run-stack.sh`,无产品路径变更(仍是唯一的 Route A,单一原生调试模式)。
