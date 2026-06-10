# AGENTS.md — cocola 多 Agent 协作约定

> 面向所有参与 cocola 开发的 AI Agent。**开始任何编码任务前，请先阅读本文件，并浏览 `tmp/dev/` 了解项目近期历史。**

## 1. 项目速览

cocola 是一个企业内部自部署的 Agent 平台（Go + Python 后端，Next.js 前端，基于 Claude Code Agent SDK）。整体定位、技术栈与仓库结构见 [`README.md`](./README.md)；架构决策见 [`docs/adr/`](./docs/adr/)。

工程约定：

- Python 项目统一用 **uv** 管理。
- 优先复用成熟开源方案，避免重复造轮子。
- 提交代码时**不跳过 git hooks**（禁止 `--no-verify`），不要 amend 他人提交。
- 本地开发栈通过 `scripts/run-stack.sh` 启停，退出后不应残留端口占用。

## 2. 代码格式化与风格检查（强制）

提交前所有代码必须经过统一格式化。机制基于 [pre-commit](https://pre-commit.com/) 框架，配置见 [`.pre-commit-config.yaml`](./.pre-commit-config.yaml)，全部为本地 hook（不依赖远程仓库，规避企业 TLS 代理）。

### 2.1 各语言工具与配置

| 语言 / 文件            | 格式化                | 风格检查 (lint)                        | 配置文件                                         |
| ---------------------- | --------------------- | -------------------------------------- | ------------------------------------------------ |
| Python                 | `ruff format`         | `ruff check --fix`                     | `ruff.toml`、`packages/py-common/pyproject.toml` |
| Go                     | `gofmt -w -s`         | `golangci-lint`（CI/Make，非每次提交） | `.golangci.yml`                                  |
| TS/JS/JSON/CSS/MD/YAML | `prettier`            | `next lint`（web，CI/Make）            | `.prettierrc.json`、`.prettierignore`            |
| Proto                  | `buf format`          | `buf lint`（Make）                     | `packages/proto/buf.yaml`                        |
| 通用                   | 去行尾空白 + 末尾换行 | —                                      | `.editorconfig`                                  |

提交时 pre-commit 仅做**快速、可自动修复**的格式化 + 轻量 lint；较慢的 `golangci-lint`、`next lint`、`buf lint` 放在 `make lint` 与 CI 中权威执行。`buf` / `prettier` 缺失时对应 hook 会优雅跳过（exit 0），不阻塞提交，安装后自动生效。

### 2.2 首次安装（每位贡献者 / 每个新环境）

```bash
pip install pre-commit          # 或 uv tool install pre-commit
make precommit-install          # 安装 .git/hooks/pre-commit
pnpm install                    # 让 prettier 可用（web 工具链）
pre-commit run --all-files      # 一次性把存量代码格式化干净
```

### 2.3 常用命令

- `make format`：一键格式化全部语言（Go / Python / web）。
- `make format-check`：只检查不改写，供 CI 用。
- `make lint`：运行全部权威 linter（golangci-lint / ruff / next lint / buf lint）。
- 提交被 hook 拦下时，hook 通常已自动改好文件，`git add` 后重新 `git commit` 即可。

> 不要用 `--no-verify` 跳过 hook（见下文工程约定）。如需临时跳过个别 hook，用 `SKIP=<hook-id> git commit`。

## 3. 变更记录机制（强制）

为支持多 Agent 协作，**每次提交代码（`git commit`）前，必须在 `tmp/dev/` 目录下生成一个 Markdown 变更记录文件**。该目录已纳入版本控制（见 `.gitignore` 例外），会随仓库一起提交。

### 3.1 文件命名（两段式）

格式：`<变更类型>-<简短描述>.md`

- **变更类型**取自约定式提交：`feat` / `fix` / `chore` / `refactor` / `docs` / `test` / `perf` / `build` / `ci`。
- **简短描述**用短横线连接，能唯一标识本次变更。
- 示例：`feat-graceful-teardown.md`、`fix-claude-config-isolation-503.md`。
- 若同名文件已存在，加日期前缀避免覆盖：`20260610-fix-xxx.md`。

### 3.2 内容要求

每个文件至少包含以下三部分：

1. **变更时间** —— 精确到分钟，带时区。
2. **变更理由**
   - 修 bug：描述 bug 现象、触发条件、根因。
   - 加功能：描述用户诉求 / 业务背景。
3. **变更内容** —— 改了哪些文件、具体做了什么、关键设计取舍。

### 3.3 模板

```markdown
# <类型>: <一句话标题>

- 变更时间：2026-06-10 14:50 (+08:00)
- 关联提交：<commit hash 或 PR 链接，可选>

## 变更理由

（修 bug 写现象+触发条件+根因；加功能写用户诉求/背景）

## 变更内容

- path/to/file_a：做了什么
- path/to/file_b：做了什么
- 关键取舍 / 注意事项
```

## 4. 如何查看项目历史

`tmp/dev/` 是项目的"人类可读变更日志"，随仓库提交。任何 Agent 在 clone 仓库后都能直接阅读。**开始新任务前建议先浏览该目录**，了解：

- 近期改了什么、为什么改；
- 已有的设计决策与踩过的坑，避免重复踩坑或推翻他人已验证的结论。

配合 `git log` 与 `docs/adr/` 一起阅读，可获得完整的演进脉络。
