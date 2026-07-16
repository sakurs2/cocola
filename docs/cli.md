# Cocola CLI

`cocola` 是正式部署和日常运维的统一入口。它是一个独立 Go 二进制，不要求目标主机
安装 Git、Go、Python、Node.js，也不要求 clone Cocola 仓库。源码开发仍使用
`make dev`，两者不混用。

## 安装

前置条件：Linux 或 macOS、Docker daemon、Docker Compose v2。支持 amd64 和 arm64。

```bash
curl -fsSL https://raw.githubusercontent.com/sakurs2/cocola/master/scripts/install.sh | sh
```

脚本只负责引导安装：识别系统架构，从 GitHub Release 下载对应 CLI，校验
`checksums.txt`，原子写入 `~/.local/bin/cocola`，再启动交互式配置。首次使用前请确保
`~/.local/bin` 在 `PATH` 中。配置完成后可先检查 `~/.cocola/config.env`，确认无误再
执行 `cocola up`。

安装指定 CLI 版本或目录：

```bash
curl -fsSL https://raw.githubusercontent.com/sakurs2/cocola/master/scripts/install.sh \
  | sh -s -- --cli-version v0.1.0 --install-dir "$HOME/bin"
```

安装脚本后面的 `--` 会把剩余参数传给 CLI。例如无交互生成配置：

```bash
curl -fsSL https://raw.githubusercontent.com/sakurs2/cocola/master/scripts/install.sh \
  | sh -s -- -- install --yes
```

## 常用命令

```text
cocola install                 交互生成并校验部署配置
cocola up                      拉取镜像并启动或更新服务
cocola down                    停止服务
cocola restart                 重启现有容器
cocola status                  查看容器状态
cocola logs [-f] [service]     查看全部或单个服务日志
cocola doctor                  检查 Docker、Compose 和安装配置
cocola version                 查看 CLI 构建版本
```

`install` 支持管理员账号、镜像 Registry/版本、Web/Gateway/LLM 端口，以及内置或
外部 OpenSandbox。使用外部 OpenSandbox 时，必须同时提供一个从远端 sandbox 可达的
LLM Gateway URL，CLI 会拒绝会产生失联 sandbox 的不完整配置。

自动化环境使用 `--yes` 跳过表单，并用命令行参数显式覆盖默认值。支持结构化输出的
命令可加 `--json`；`logs` 是原始字节流，不支持 JSON。设置 `NO_COLOR=1`、
`TERM=dumb` 或 `--no-color` 会关闭 ANSI 样式；非 TTY 输出也会自动关闭颜色。

## 安装数据

默认目录是 `~/.cocola`，可用全局 `--home` 或 `COCOLA_HOME` 修改：

```text
~/.cocola/
├── compose.yaml    CLI 内嵌的正式 Compose，不依赖源码目录
├── config.env      0600，镜像、端口和生成的 Secret
├── state.json      0600，CLI 管理状态
└── sandboxes/      OpenSandbox Docker runtime 的宿主目录
```

重复执行 `install` 不会覆盖现有配置或 Secret。`install` 不会拉取镜像或启动服务；
用户检查配置后显式执行 `cocola up`。自定义安装目录后，后续命令需继续使用同一个
`--home`，或设置 `COCOLA_HOME`。

内置 OpenSandbox 模式下，`cocola down` 会先停止对话入口和 Agent Runtime，再清理
本次安装产生的动态 sandbox，最后移除 Compose 服务。Compose 属于 legacy Docker
部署，Session 文件直接保存在 `~/.cocola/sandboxes` 的宿主机目录；不使用
checkpoint 或 Warm Pool。外部 OpenSandbox 由调用方管理，CLI 不会清理其中的
sandbox。

## 开发与发布

本地构建和测试：

```bash
go build -o bin/cocola ./apps/cli/cmd/cocola
cd apps/cli && go test ./...
```

推送版本 tag 后，Release workflow 会先校验版本，再构建 linux/darwin、amd64/arm64
CLI 以及同版本全套服务镜像；镜像成功后才发布 CLI Release。正式版本必须使用
`vMAJOR.MINOR.PATCH`（如 `v2.0.0`）并高于历史最新正式版本；预发布版本使用
`vMAJOR.MINOR.PATCH-prerelease`（如 `v2.0.0-rc.1`）并按顺序递增。非法、回退或已经
发布过的版本会在任何镜像构建前失败。安装脚本的归档文件名与 GoReleaser 固定为
`cocola_<goos>_<goarch>.tar.gz`。
