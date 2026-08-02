# fix: 修复 Compose 沙箱路径与 Docker endpoint 兼容

- 变更时间：2026-08-02 20:23 (+08:00)

## 变更理由

正式 Compose 部署把 `COCOLA_SANDBOX_ROOT` 传给 sandbox-manager，却没有把宿主目录
以相同绝对路径挂入该容器。sandbox-manager 因此可能在自身容器层创建 Session 目录，而
OpenSandbox 随后从宿主 Docker daemon 挂载同名路径，造成空目录、沙箱创建失败或持久化
失效。

Compose 还把 `/var/run/docker.sock` 写死为 host-agent 与 OpenSandbox 的 socket 来源，
但 CLI 实际使用当前 Docker context。rootless Docker、OrbStack 或其他自定义 Unix endpoint
可能通过 CLI daemon 检查，却无法让服务访问同一个 daemon；远程 TCP/SSH context 则无法
满足当前 host bind mount 的本地路径契约。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`：把沙箱根目录以 source==target 方式挂入
  sandbox-manager；host-agent 与 OpenSandbox 改用 CLI 注入的 Docker socket source。
- `apps/cli/internal/compose/runner.go`：按照 `DOCKER_CONTEXT`、`DOCKER_HOST` 与当前 context
  解析本地 Unix Docker endpoint，在每个 Runner 内缓存并注入所有 Compose 命令；远程或
  非法 endpoint 在创建服务前返回英文诊断。
- `apps/cli/internal/command/preflight.go`：启动前创建并验证沙箱根目录可写，避免 Compose
  静默生成错误路径。
- `apps/cli/internal/doctor/doctor.go`：展示实际解析到的 Docker endpoint，并把不兼容的远程
  endpoint 标记为失败。
- `apps/cli/internal/*_test.go`：覆盖路径同构、动态 socket 注入、rootless/custom Unix
  endpoint、context 优先级、远程 endpoint 拒绝与沙箱目录预检。

## 关键取舍

- 不新增安装向导选项，也不把运行时 socket 写入用户配置；每次 CLI 操作依据当前 Docker
  环境自动解析。
- 容器内仍统一使用 `/var/run/docker.sock`，仅动态调整 bind source，避免修改 host-agent
  和 OpenSandbox 的运行时约定。
- 当前 Session Storage 依赖本地 host bind mount，因此明确拒绝 TCP/SSH 远程 Docker
  context，避免启动成功后静默挂载错误宿主路径。
