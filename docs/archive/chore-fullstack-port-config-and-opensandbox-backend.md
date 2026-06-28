# chore: full.yml 端口配置化 + 全栈可切 OpenSandbox 后端

日期：2026-06-28 · 关联 Plan：docs/plan/m-full-port-config-and-opensandbox-backend.md
关联 ADR：0013/0014/0015

## 背景
用户要在 `docker-compose.full.yml` 全栈下验证 **OpenSandbox** 沙箱后端。原文件三处障碍：
provider 写死 `docker`、sandbox-manager 未透传 OpenSandbox env、admin-api 与
OpenSandbox server 都发布宿主 :8090 冲突；此外所有宿主端口都是硬编码字面量。

## 改动
- **deploy/docker-compose/docker-compose.full.yml**
  - 所有宿主发布端口参数化为 `${COCOLA_*_HOST_PORT:-默认}`（redis/pg/sandbox-manager/
    llm-gateway/admin-api/agent-runtime/gateway/web）。容器内监听端口与服务名互访不变。
  - `COCOLA_SANDBOX_PROVIDER` 改 `${...:-docker}`（默认仍 docker）。
  - sandbox-manager 透传 `COCOLA_OPENSANDBOX_URL/_API_KEY/_DEFAULT_CPU/_DEFAULT_MEMORY/
    _DIRECT_EXEC`（均空缺省，不影响 docker 路径）。
  - **admin-api 宿主端口默认 8090 → 8092**，把 :8090 让给 OpenSandbox server；
    容器内仍 :8090，agent-runtime 经 `http://admin-api:8090` 访问，host-side only。
- **deploy/docker-compose/docker-compose.opensandbox.yml**
  - server 宿主端口参数化 `${COCOLA_OPENSANDBOX_HOST_PORT:-8090}`。
- **.env.example**
  - 新增「沙箱后端 / OpenSandbox」「宿主端口覆盖」两节，给出切 opensandbox 的最小配置。

## 净效果
- 默认零配置：provider=docker，admin-api 宿主=8092，full 栈与 opensandbox 栈可同时 up。
- 切后端只改 `.env` 三行（provider + URL[+API_KEY]），**无需任何 override 文件**。

## 校验
- `docker compose -f docker-compose.full.yml --env-file /dev/null config` 解析通过；
  默认 published：admin=8092、provider=docker、OPENSANDBOX_URL 空。
- 覆盖 env（provider=opensandbox + URL + COCOLA_ADMIN_HOST_PORT=8099）后 config 反映正确。
- `docker compose -f docker-compose.opensandbox.yml config` 解析通过，published=8090。

## 非目标
- 不改 Go 代码（provider env 名沿用 opensandbox.go 既有）。
- 不实现 opensandbox WriteFile/ReadFile（另案）。
