# Plan: sandbox-runtime 镜像改为 FROM opensandbox/code-interpreter

- Status: Done (built & smoke-tested 2026-06-28)
- Date: 2026-06-28
- Owner: @wangjiahui
- Related: ADR-0009 (Route A 在沙箱内跑 brain)、ADR-0014（沙箱后端收敛到 OpenSandbox）

## 背景 / 动机

当前 `deploy/sandbox-runtime/Dockerfile` 从 `ubuntu:22.04` 从零构建:apt 装
OS 工具链 → nodesource 装 Node → `curl https://astral.sh/uv/install.sh` 装 uv
→ 烘焙 claude CLI + claude-agent-sdk venv + shim + 防火墙。其中 astral.sh 那一步
在受限网络下会卡死。

cocola 的设计约束是「尽量复用开源项目,避免造轮子」。OpenSandbox 已经是 cocola
选定的沙箱后端(ADR-0014),其官方 `code-interpreter` 镜像本身就是为「在沙箱里跑
Claude Code / Gemini CLI / Codex」准备的运行时底座。直接 `FROM` 它,既消除卡死的
uv 安装步骤,又复用了官方维护的多语言运行时,符合复用约束;同时仍是 **构建期烘焙**
(不是运行时安装),不违反 ADR-0009「CLI 必须预烘焙」。

## 镜像 inspect 结论(opensandbox/code-interpreter:v1.1.0, arm64)

| 组件 | 基础镜像是否自带 | 处置 |
|---|---|---|
| node v22.2.0 / npm 10.7.0 | 是(`/opt/node/v22.2.0/bin`,在非登录 sh PATH 中) | 删 nodesource 层 |
| uv 0.11.19 | 是(`/usr/local/bin/uv`) | **删卡死的 astral.sh 层** |
| python3 3.12.3 + venv 模块 | 是 | 删 apt python 层 |
| git / curl / apt-get | 是 | 复用 apt 装其余工具 |
| claude CLI | 否 | 保留烘焙层(offline tgz 优先 + registry 兜底) |
| claude-agent-sdk venv | 否 | 保留 `uv venv` 层 |
| tini / iptables / ipset / ripgrep / jq | 否 | 改用 apt 安装(新增一层) |
| 默认 User | root | 与现状一致 |
| 默认 WorkingDir | /workspace | 与 cocola 一致 |
| 默认 ENTRYPOINT | code-interpreter.sh(后台起 jupyter,仅 127.0.0.1:44771) | cocola 覆盖 ENTRYPOINT/CMD,不会触发 |

需要权衡/标注的点:
- **体积约 7GB**(多语言 + 多版本 runtime)。比 ubuntu 自建大很多。可接受:这是
  沙箱业务镜像,本地构建一次后复用;后续如需瘦身可单列任务。在 README 标注。
- **不监听端口**:基础镜像默认 entrypoint 会起 jupyter,但 cocola 用自己的
  `firewall-entrypoint.sh` 覆盖 ENTRYPOINT+CMD,jupyter 不会启动 → 仍满足「沙箱
  绝不监听网络端口」硬规则。
- **Python 版本**:基础镜像系统 python3 是 3.12;现有 venv 层固定 `--python 3.11`。
  base 自带 uv 可按需拉 3.11,或直接用 3.12。决策见下。

## 改动范围

仅 `deploy/sandbox-runtime/Dockerfile`(+ README 同步 + changelog)。不动 provider、
agent-runtime、compose。

### Dockerfile 改写要点
1. `FROM ubuntu:22.04` → `FROM opensandbox/code-interpreter:v1.1.0`
   - 用变量 `ARG OPENSANDBOX_BASE=opensandbox/code-interpreter:v1.1.0` 便于 CI 换 tag/镜像源。
2. 删除:apt OS 工具链层中已自带的部分、nodesource 层、astral.sh uv 安装层、
   python 软链层。
3. 新增/保留一层 apt:`tini iptables ipset ripgrep jq`(base 没有,防火墙+shim 需要)。
   - 保留 `ca-certificates`(应已有,apt 幂等)。
4. venv 层:把 `--python 3.11` 改为 `--python 3.12`(对齐 base 系统 python,避免 uv
   再下载一份 3.11 拖慢构建/增体积)。同步改 ARG 默认 `PYTHON_VERSION=3.12`。
   - shim entrypoint 直接 exec `/opt/cocola/venv/bin/python`,与系统 python 版本无关,
     改 3.12 不影响 shim 契约。
5. claude CLI 烘焙层:保持 offline tgz 优先 + `npm install -g @anthropic-ai/claude-code`
   兜底。base 已有 npm。
6. firewall + shim + 非 root user(uid 10001 cocola)+ ENV + WORKDIR + ENTRYPOINT/CMD:
   全部保留,逻辑不变。
7. 删除已无意义的 ARG `NODE_MAJOR`、`NPM_REGISTRY`(node 来自 base;registry 兜底可
   保留 npm config 但 base 默认 registry 即可——保留 `NPM_REGISTRY` ARG 以便离线镜像源)。
   决策:保留 `NPM_REGISTRY`(离线场景有用),删 `NODE_MAJOR`(node 由 base 决定)。

## 验证

- `docker build -t cocola/sandbox-runtime:dev deploy/sandbox-runtime`(确认不再卡 uv)。
- 构建后:`docker run --rm --entrypoint bash <img> -lc 'claude --version; \
  /opt/cocola/venv/bin/python -c "import claude_agent_sdk"; which tini iptables ipset rg jq node uv'`。
- 确认 ENTRYPOINT 覆盖生效、容器不监听端口(覆盖 CMD 后无 jupyter)。
- git 钩子绿;不提交 `.claude/`;补 `docs/archive/` changelog。

## 不做(本次范围外)
- 镜像瘦身(多语言裁剪)。
- opensandbox provider 的 WriteFile/ReadFile 实现(仍 errNotImplemented)。
- 端到端 chat 验收(依赖镜像构建成功后另起)。
