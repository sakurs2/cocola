# chore: sandbox-runtime 镜像改为 FROM opensandbox/code-interpreter

- Date: 2026-06-28
- Author: @wangjiahui
- Plan: docs/plan/sandbox-runtime-base-on-opensandbox-image.md
- Related: ADR-0009 (Route A 沙箱内 brain)、ADR-0014 (沙箱后端收敛 OpenSandbox)

## 背景

`deploy/sandbox-runtime/Dockerfile` 原从 `ubuntu:22.04` 从零构建,其中
`curl https://astral.sh/uv/install.sh` 安装 uv 一步在受限网络下卡死。cocola 既然
已选 OpenSandbox 作为沙箱后端,其官方 `code-interpreter` 镜像本身就是为「沙箱内跑
Claude Code / Gemini CLI / Codex」准备的运行时底座,直接复用即可消除卡死步骤,并落实
「尽量复用开源、避免造轮子」约束。仍是构建期烘焙,不违反 ADR-0009「CLI 必须预烘焙」。

## 改动

- `deploy/sandbox-runtime/Dockerfile`
  - `FROM ubuntu:22.04` → `FROM ${OPENSANDBOX_BASE}`(默认
    `opensandbox/code-interpreter:v1.1.0`,可 `--build-arg` 换 tag/离线镜像源)。
  - 删除基础镜像已自带的层:apt OS 工具链中重复部分、nodesource 装 Node、
    **卡死的 astral.sh 装 uv**、python 软链。
  - 新增一层 apt 只补基础镜像缺的:`tini iptables ipset ripgrep jq`
    (+ `ca-certificates`,幂等)。
  - venv Python 版本参数从 `PYTHON_VERSION` 改名为 `COCOLA_VENV_PYTHON`(默认 3.12):
    基础镜像设了 `ENV PYTHON_VERSION=3.14`,同名 ARG 在 RUN 展开时会被 ENV 遮蔽导致
    venv 落到 3.14;改名后构建确定性恢复。
  - 删除已无意义的 `ARG NODE_MAJOR`(Node 由基础镜像决定);保留 `NPM_REGISTRY`(离线源)。
  - 显式注释:覆盖 ENTRYPOINT+CMD,使基础镜像默认的 jupyter 启动器不会运行 → 仍满足
    「沙箱绝不监听网络端口」硬规则。
- `deploy/sandbox-runtime/README.md`:补 FROM opensandbox 说明 + 更新 Layout 注释。
- `docs/plan/sandbox-runtime-base-on-opensandbox-image.md`:新增 Plan,标 Done。

## 验证

- `DOCKER_BUILDKIT=0 docker build --build-arg OPENSANDBOX_BASE=<本地已拉镜像> \
  -t cocola/sandbox-runtime:dev deploy/sandbox-runtime` 全 23 步成功,uv 步骤不再卡。
  (BuildKit 因本机到 docker.io 的 TLS 证书问题拉不到 `docker/dockerfile:1` frontend,
  故用 legacy builder;`# syntax=` 指令对 legacy 无影响。)
- `docker run` 冒烟:claude 2.1.195、claude-agent-sdk 0.2.110、venv Python 3.12.13、
  node v22.2.0、uv 0.11.19、tini/iptables/ipset/rg/jq 均就位,cocola 用户 uid=10001,
  shim entrypoint 存在。
- 镜像体积 7.91GB(基础镜像 ~7GB + cocola 层 ~0.9GB)。瘦身另列,本次范围外。

## 范围外 / 后续

- 镜像瘦身(裁剪基础镜像的多语言/多版本 runtime)。
- opensandbox provider 的 WriteFile/ReadFile(仍 errNotImplemented)。
- 端到端 chat 验收(现在有可用 brain 镜像,可另起)。
