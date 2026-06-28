# 把"部署 server"并入 OpenSandbox e2e 测试命令

日期: 2026-06-28

## 背景

`make verify-opensandbox` 只会跑 harness(`cmd/opensandbox-verify`),它假设
`COCOLA_OPENSANDBOX_URL` 指向的 OpenSandbox server 已经在监听。实测当 :8090 上
没有 server 时,会直接 `dial tcp [::1]:8090: connect: connection refused` ——
被测对象(server)本身没起。

诉求:测试命令应当自包含,把"部署 server"也涵盖进去。

## 改动

### 1. 新增 compose: `deploy/docker-compose/docker-compose.opensandbox.yml`

把"被测对象"——一台带 Docker runtime 的 OpenSandbox FastAPI server——固化成
一个 compose 文件:

- 复用上游官方镜像 `opensandbox/server:latest`,不 fork、不重建(符合"优先复用
  开源项目"约束);execd / egress sidecar 镜像由 server 按需拉取。
- 监听 `0.0.0.0:8090`,端口映射 `8090:8090`,正好对上 harness 默认
  `COCOLA_OPENSANDBOX_URL=http://localhost:8090/v1`。
- 挂 `/var/run/docker.sock`,docker runtime 以宿主 daemon 的兄弟容器方式拉起沙箱。
- 默认**关闭鉴权**(`api_key` 留空 + `OPENSANDBOX_INSECURE_SERVER=YES`),harness
  无需 API key 即可跑;要验鉴权则取消注释 `[server].api_key` 并 export
  `COCOLA_OPENSANDBOX_API_KEY` 同值。
- `[docker] host_ip = "host.docker.internal"`,规避 bridge 模式下 server 在容器内
  解析不到宿主、exec/文件回连失败的已知坑。
- healthcheck 打 `/health`(server 中间件里该路径免鉴权)。

### 2. Makefile 新增三个目标

- `opensandbox-up`:`docker compose up -d` 起 server,并轮询 `/health`(最多 ~120s)
  直到健康;超时则打印 server 日志并失败。
- `opensandbox-down`:`docker compose down` 停并清理。
- `verify-opensandbox-full`:依赖 `opensandbox-up`,起好服务后再调
  `verify-opensandbox` 跑 harness。server 跑完保留,便于复跑/排查。

`verify-opensandbox`(纯 harness,沿用 `cd apps/sandbox-manager && GOWORK=off`)保持
不变,适合 server 已在别处运行的场景。

## 完整测试命令(一条龙)

```bash
# 前提:Docker daemon 在跑
make verify-opensandbox-full     # 起 server -> 等 health -> 跑 harness
make opensandbox-down            # 跑完拆台
```

要验鉴权:先在 compose 的 `[server].api_key` 填值,再
`export COCOLA_OPENSANDBOX_API_KEY=<同值>` 后执行上面命令。

## 验证

- `make -n verify-opensandbox-full` 干跑:目标链路正确(up -> 轮询 /health ->
  递归 `make verify-opensandbox` -> `GOWORK=off go run ./cmd/opensandbox-verify`)。
- 实际起容器 + 真 server 端到端跑在合规环境执行(见 #22)。本沙箱禁止启动监听进程,
  故此处不实跑 compose。
