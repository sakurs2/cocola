# fix: 使用隔离用户 venv 提供 Sandbox pip

- 变更时间：2026-08-08 00:21 (+08:00)
- 关联失败：sandbox-runtime-image run 31195131473、CI run 31195131533

## 变更理由

Sandbox Runtime 镜像尝试通过 `uv pip install --system` 向 Ubuntu 24.04 的系统 Python 安装 pip，被 PEP 668 的 `EXTERNALLY-MANAGED` 保护拒绝，导致 amd64 构建失败并取消 arm64 构建。直接使用 `--break-system-packages` 虽可绕过保护，但会允许用户依赖污染发行版管理的 Python 环境，也可能影响基础镜像组件。

同一提交的 agent-runtime 全量测试还发现两个 MCP shim 测试使用的 Fake Claude SDK 缺少新增的 `HookMatcher` 测试桩；相关功能实现本身没有失败，但测试无法构造 Execute Mode 的 Bash 输出 hooks。

## 变更内容

- `deploy/sandbox-runtime/Dockerfile`：移除系统 Python 的 pip 安装；创建由 `cocola` 用户持有的 `/home/cocola/.venv`，使用 `uv venv --seed` 提供 `python`、`python3`、`pip` 和 `pip3`，并把该环境放到用户命令 PATH 前面。
- `/opt/cocola/venv` 继续只承载平台 Agent SDK，用户执行 `pip install` 不会修改平台依赖或绕过 PEP 668。
- `scripts/sandbox-runtime-verify.sh`：增加镜像自检，确认四个 Python 命令均解析到用户 venv，且 site-packages 对非 root 用户可写。
- `apps/agent-runtime/tests/test_agent_shim_mcp.py`：为两处 Fake SDK 补齐 `HookMatcher`，覆盖 CI 全量测试路径。

## 验证

- agent-runtime：321 passed，2 skipped。
- 官方 OpenSandbox arm64 基础镜像：以 `cocola` 用户成功创建 venv，并验证 `python -m pip`、`pip`、`pip3`。
- Docker BuildKit：Dockerfile 构建检查通过，无警告。
- amd64 的首次本地基础镜像拉取长时间无进度后终止；远端失败日志已确认 amd64 基础镜像可正常运行 Python 3.12 和 uv，新的 venv 构建命令与已实测的 arm64 路径一致。
