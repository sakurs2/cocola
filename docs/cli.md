# Cocola CLI

`cocola` 是正式部署和日常运维的统一入口。它是一个独立 Go 二进制，不要求目标主机
安装 Git、Go、Python、Node.js，也不要求 clone Cocola 仓库。源码开发仍使用
`make dev`，两者不混用。

## 安装

前置条件：Linux 或 macOS、Docker daemon、Docker Compose 2.23.1 或更高版本。支持
amd64 和 arm64。

```bash
curl -fsSL https://raw.githubusercontent.com/sakurs2/cocola/master/scripts/install.sh | sh
```

脚本负责安装或更新 CLI：识别系统架构，从 GitHub Release 下载对应版本，校验
`checksums.txt`，再原子写入 `~/.local/bin/cocola`。首次安装会启动默认的交互式配置向导；
已有部署则跳过向导并准备配置迁移。首次向导会
展示 Cocola 艺术字 Logo，并依次配置管理员账号和服务端口；安全密钥
会自动生成。所有项目都有默认值，标准安装可以直接逐步确认。首次使用前请确保
`~/.local/bin` 在 `PATH` 中。

安装指定 CLI 版本或目录：

```bash
curl -fsSL https://raw.githubusercontent.com/sakurs2/cocola/master/scripts/install.sh \
  | sh -s -- --cli-version v0.1.0 --install-dir "$HOME/bin"
```

## 常用命令

```text
cocola install                 首次交互配置；已有安装则准备升级
cocola start                   校验环境并创建、更新或恢复服务
cocola stop                    停止服务但保留容器和网络
cocola status                  查看容器状态
cocola logs [-f] [service]     查看全部或单个服务日志
cocola doctor                  检查服务、磁盘、数据卷、镜像和安装配置
cocola version                 查看 CLI 构建版本
```

`install` 默认进入英文交互式向导，主要配置管理员账号和 Web/Gateway/LLM/Internal SCM 端口；内部认证、
加密、数据库、对象存储和 SCM 密钥会自动生成并写入仅当前用户可读的配置文件。Web 默认监听
`0.0.0.0`，浏览器可直接使用 `http://<server-ip>:<web-port>`，Workspace WebSocket 会按当前
请求 Host 做同源校验，不需要额外配置。`--public-url https://cocola.example.com` 保留给会改写
Host 的反向代理，以及 GitHub/飞书等需要生成固定外部回调或跳转地址的集成。
CLI 使用 Cocola 发布仓库及依赖项目的上游 Registry 下载镜像，不提供内置公共代理选择，也不修改 Docker daemon 配置。
`--registry` 继续作为只覆盖 Cocola 自有镜像仓库的高级选项；第三方镜像保持预设的直接来源。镜像版本以及外部 OpenSandbox 等高级场景仍可使用命令行参数覆盖。使用外部
OpenSandbox 时，必须同时提供从远端 sandbox 可达的 LLM Gateway URL 和 Internal SCM URL
（`--sandbox-internal-scm-url`），CLI 会拒绝
会产生失联 sandbox 的不完整配置。

支持结构化输出的命令可加 `--json`；`logs` 是原始字节流，不支持 JSON。设置 `NO_COLOR=1`、
`TERM=dumb` 或 `--no-color` 会关闭 ANSI 样式；非 TTY 输出也会自动关闭颜色。

## 安装数据

默认目录是 `~/.cocola`，可用全局 `--home` 或 `COCOLA_HOME` 修改：

```text
~/.cocola/
├── compose.yaml    CLI 内嵌的正式 Compose（含 OpenSandbox 配置），不依赖源码目录
├── config.env      0600，镜像、端口和生成的 Secret
├── state.json      0600，CLI 管理状态
├── .operation.lock install/start/stop 的安装目录级操作锁
├── backups/        升级前的部署、Cocola PostgreSQL 与 Internal SCM 一致性备份
└── sandboxes/      OpenSandbox Docker runtime 的宿主目录
```

首次安装完成页会明确显示生成的
`config.env` 绝对路径、管理员登录信息和 `cocola start` 下一步命令。`install` 不会拉取
镜像或启动服务；用户检查配置后显式执行 `cocola start`。自定义安装目录后，后续命令需
继续使用同一个 `--home`，或设置 `COCOLA_HOME`。

`cocola start` 是唯一启动入口：它会检查 Docker、Compose、部署配置、首次启动端口和基本
磁盘空间。首次启动或待应用升级时会拉取 Compose 服务镜像，以及 Managed OpenSandbox 使用的
Sandbox Runtime、execd 和 egress 镜像。Registry 不可用但所有目标镜像已缓存时继续启动；
缓存不完整则明确失败，不会自动切换到其他 Registry。普通
`stop` 后恢复不会强制访问 Registry。首次启动发现已有 PostgreSQL 数据卷时，会先验证当前配置
能否通过密码认证：兼容的中途安装继续启动，不兼容的遗留卷会停止并给出保留或清理数据的明确
指引，CLI 不会自动删除数据。随后通过 Compose `up --wait` 创建缺失容器、重建配置或镜像发生
变化的服务、恢复已停止容器，并等待包含 Sandbox Manager、Agent Runtime 和 Web 在内的健康
检查通过。成功页会显示当前版本、Web、Admin 和模型配置入口；升级成功时同时显示
`Before version` 与 `Current version`。失败页会展示当前容器状态及诊断命令。

`install`、`start` 和 `stop` 在同一个安装目录上串行执行。如果另一个变更操作正在运行，CLI
会显示其命令、PID 和开始时间并立即退出；`status`、`logs` 和只读的 `doctor` 仍可用于观察。
`doctor` 会检查容器状态、数据卷、当前 PostgreSQL 凭据、本地镜像缓存、安装目录磁盘、可见的
Docker Root Dir，以及 Internal SCM 配置端口是否确实由 Forgejo 容器持有，不会启动容器、拉取镜像
或删除资源。

## 升级

升级不增加新的用户命令。重新执行安装命令即可下载新版 CLI：

```bash
curl -fsSL https://raw.githubusercontent.com/sakurs2/cocola/master/scripts/install.sh | sh
cocola start
```

检测到已有 `config.env` 后，`install` 会跳过首次向导，根据独立的配置 Schema 执行迁移。
管理员账号、端口、Secret、已知配置值和额外环境变量都会保留；CLI 管理的 Compose 和
目标镜像版本会更新。准备升级只显示 `Before version` 与 `New version`，直到 `start` 健康检查成功后才将新版本
称为 `Current version`。修改前的文件保存在
`~/.cocola/backups/upgrade-<time>-<from>-to-<to>/`。

应用升级时，如果当前安装已有 PostgreSQL 数据卷，`start` 会先生成 owner-only 的
`postgres.dump`；存在 Internal SCM 时还会短暂停止其写入入口并生成
`forgejo-postgres.dump` 与 `forgejo-data.tar.gz`，随后恢复原服务，再拉取和启动目标版本。
目标版本拉取或健康检查失败时，CLI 会恢复旧部署
文件，但不会在失败处理过程中继续编排容器。失败输出会分别给出 `cocola start` 恢复上一版，
以及重新执行 `cocola install --version <target>` 后再 `cocola start` 重试目标升级的命令。
数据库与 Forgejo 数据不会被自动还原，数据备份和部署备份始终保留，供人工恢复。第三方
基础镜像使用固定版本，避免一次普通重启隐式升级 Redis、PostgreSQL、MinIO 或 OpenSandbox。

`cocola stop` 会先停止 Web、Gateway 和 Agent Runtime，避免产生新任务；Sandbox Manager
随后在 30 秒预算内通过 Provider API 销毁 Redis 已登记的运行中 sandbox，最后执行 Compose
`stop`。服务容器、默认网络、镜像、具名数据卷和 `~/.cocola/sandboxes` 中的 Session 文件均
保留。Kubernetes 模式下同一流程会删除对应的运行中 Sandbox Pod，保留 Session PVC。CLI
不会仅凭镜像名扫描并强制删除宿主机容器；未登记或 Provider 超时的异常残留需要管理员根据
日志核对后处理。

## 开发与发布

本地构建和测试：

```bash
go build -o bin/cocola ./apps/cli/cmd/cocola
cd apps/cli && go test ./...
```

推送版本 tag 后，Release workflow 会先校验版本，再构建 linux/darwin、amd64/arm64
CLI 以及同版本全套服务镜像；同时将固定 digest 的未修改 Forgejo 多架构镜像同步到 Cocola GHCR，
验证匿名读取，并把对应完整源码和许可证作为 Release 资产发布；这些步骤成功后才发布 CLI Release。正式版本必须使用
`vMAJOR.MINOR.PATCH`（如 `v2.0.0`）并高于历史最新正式版本；预发布版本使用
`vMAJOR.MINOR.PATCH-prerelease`（如 `v2.0.0-rc.1`）并按顺序递增。非法、回退或已经
发布过的版本会在任何镜像构建前失败。正式版本同时更新 `latest` 镜像，源码构建的开发版
CLI 默认使用该通道；预发布版本只发布自己的版本 tag，不覆盖 `latest`。每个镜像必须通过
匿名拉取检查后才会发布 CLI Release；首次创建的 GHCR Package 需要由维护者设置为 Public。
安装脚本的归档文件名与 GoReleaser 固定为 `cocola_<goos>_<goarch>.tar.gz`。
