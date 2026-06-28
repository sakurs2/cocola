# Plan：端口配置化 + OpenSandbox 后端全栈验证填坑

状态：进行中 · 日期：2026-06-28 · 关联：ADR-0013/0014/0015、docker-compose.full.yml

## 背景与动机
用户要在 `docker-compose.full.yml` 全栈下验证 **OpenSandbox** 沙箱后端（而非默认 docker
后端）。当前有三个障碍：

1. **provider 写死**：`sandbox-manager.COCOLA_SANDBOX_PROVIDER: "docker"` 是字面量，
   无法不改文件就切到 opensandbox；且 sandbox-manager 没有透传
   `COCOLA_OPENSANDBOX_URL/_API_KEY` 等 provider 必需 env，opensandbox.New() 拿不到 baseURL。
2. **端口冲突**：admin-api 与 `docker-compose.opensandbox.yml` 的 opensandbox-server
   **都发布宿主 :8090**，两者同时起会撞。
3. **端口全硬编码**：full.yml 里 redis/pg/gateway/web/... 宿主端口都是字面量，
   改端口要动文件，不符合「配置文件优先」。

复用约束：compose 已有 `${VAR:-default}` 惯例 + 仓库已入库 `.env.example`（`.env` 被
gitignore）。不引入新机制，直接沿用。

## 方案
### A. 端口配置化（full.yml）
所有宿主侧发布端口改为 `${COCOLA_*_PORT:-默认值}:容器端口`：
- redis 6379、postgres 5432、sandbox-manager 50051、llm-gateway 18091→8080、
  admin-api 8090→8090、agent-runtime 50061、gateway 8080、web 3000。
- 容器内部监听端口与服务名互访（如 `admin-api:8090`、`agent-runtime:50061`）保持不变，
  仅参数化「宿主发布端口」。这样改端口零代码改动、零服务间断链。

### B. 解端口冲突
- admin-api 宿主发布端口默认从 8090 → **8092**（`COCOLA_ADMIN_HOST_PORT:-8092`）；
  容器内 `:8090` 不变，agent-runtime 经 `http://admin-api:8090` 访问，不受影响。
- opensandbox-server 保持 8090（`docker-compose.opensandbox.yml` 也参数化为
  `COCOLA_OPENSANDBOX_HOST_PORT:-8090`），`make verify-opensandbox-full` 等既有路径零破坏。
- 结论：默认值即可两栈共存，无需任何 override 文件。

### C. provider 切换 + env 透传(full.yml sandbox-manager)
- `COCOLA_SANDBOX_PROVIDER: "${COCOLA_SANDBOX_PROVIDER:-docker}"`（默认仍 docker）。
- 透传：`COCOLA_OPENSANDBOX_URL`、`COCOLA_OPENSANDBOX_API_KEY`、
  `COCOLA_OPENSANDBOX_DEFAULT_CPU`、`COCOLA_OPENSANDBOX_DEFAULT_MEMORY`、
  `COCOLA_OPENSANDBOX_DIRECT_EXEC`（均 `${...:-}` 空缺省，不影响 docker 路径）。
- sandbox-manager 在 compose 网络内，访问宿主上的 opensandbox-server 须走
  `http://host.docker.internal:<HOST_PORT>/v1`（注意 `/v1` 后缀，与 verify-full 一致）。

### D. .env.example 增补
新增「沙箱后端 / OpenSandbox」与「端口覆盖」两节，给出切 opensandbox 的最小三行：
provider=opensandbox + URL（host.docker.internal:8090/v1）+（可选）API_KEY。

## 验收
1. `docker compose -f docker-compose.full.yml config` 与 opensandbox.yml `config` 均能解析。
2. 不设任何 env：provider=docker、admin-api 宿主端口=8092、其余端口默认值不变。
3. 设 `COCOLA_SANDBOX_PROVIDER=opensandbox` + URL：sandbox-manager 环境出现这些 env。
4. 两栈可同时 up（8090 给 opensandbox-server、8092 给 admin-api）。

## 非目标
- 不改 Go 代码（provider env 名沿用现有 opensandbox.go）。
- 不动 docker 默认路径行为。
- 不实现 opensandbox WriteFile/ReadFile（另案）。
