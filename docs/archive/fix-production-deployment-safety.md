# fix: 加固 Cocola 生产部署入口

- 变更时间：2026-08-01 20:08 (+08:00)

## 变更理由

正式安装生成的 Origin 仅包含 localhost，通过服务器 IP、域名或 HTTPS 访问时，Web
虽然可能成功监听端口，但 Workspace WebSocket 会被精确 Origin 校验拒绝，code-server
也会再次拒绝未知外部 Host。管理员密码校验与 admin-api 的 bcrypt 约束也不一致，部分
密码会在安装成功后导致首次启动失败。

部署前置检查只确认 Docker Compose v2 存在，没有验证内嵌 `configs.content` 所需的
2.23.1 最低版本。此外，旧 CLI 停机链路曾按 Sandbox 镜像扫描并强制删除宿主机容器，
无法证明这些容器属于当前安装，存在误删其他工作负载的风险；仅删除该扫描后，Sandbox
Manager 退出又不会主动释放运行实例，会遗留 Docker sandbox 或 Kubernetes Pod。

## 变更内容

- `apps/cli/internal/config`、`apps/cli/internal/command`：保留 `--public-url` 高级覆盖和严格的
  http(s) Origin 校验，但从默认交互向导移除额外访问地址步骤；管理员密码统一限制为至少
  8 个字符且不超过 72 个 UTF-8 字节。
- `apps/admin-api/internal/service`：对创建、重置和 Bootstrap 密码应用相同约束，并更新
  HTTP/API 测试数据。
- `apps/web/server.mjs`、`apps/web/lib/public-origins.mjs`、`apps/gateway/internal/httpapi`、
  Sandbox Provider：生产和 `make dev` 均默认监听 `0.0.0.0`；Workspace WebSocket 接受
  Origin 与请求 Host 同源的服务器 IP/域名；Gateway 仅为 code-server 改写固定内部 Origin，
  任意 Preview 服务仍保留浏览器原始 Origin。
- `apps/cli/internal/compose`、`apps/cli/internal/doctor`：解析 Compose 语义版本，在启动前
  要求 2.23.1 或更高版本，并在 doctor 结果中展示检测版本和最低版本。
- `apps/sandbox-manager/internal/orchestrator`、Sandbox Manager 入口与正式 Compose：进程停止
  接单后，在有界时间内经 Provider 销毁 Redis 已登记的计算实例并移除运行绑定，不删除
  Session Storage/PVC；为 Compose 配置 45 秒 stop grace period。
- `apps/cli/internal/compose/runner.go`：移除仅按镜像名执行的全局 `docker rm -f` 兜底；内置
  OpenSandbox 停机时先停止入口和执行器、再让 Sandbox Manager Drain，最后停止 Compose
  服务但保留容器与网络；异常残留改由管理员确认所有权后处理。
- `README.md`、`docs/cli.md`、`.env.example`：同步生产域名、监听地址、Compose 版本和安全
  停机说明。

## 关键取舍

- 本次不实现多安装实例隔离，当前 Compose project name 仍保持兼容；只移除无法安全判断
  所有权的破坏性清理。
- 本次不改变宿主 Shell 环境变量相对 `config.env` 的优先级。
- `0.0.0.0` 仅作为监听地址，不能用作浏览器 Public URL；默认 IP/域名访问依赖 Origin 与
  请求 Host 精确同源，不引入通配符。
- Drain 只处理 Redis 注册表中可证明由 Cocola 管理的计算实例，并保留 Session 数据；超时
  或未登记的孤儿工作负载不会通过模糊镜像匹配强删。
