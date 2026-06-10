# feat: 引入 pre-commit 多语言格式化与风格检查

- 变更时间：2026-06-10 15:10 (+08:00)

## 变更理由
用户诉求：对代码格式有严格要求，希望在 pre-commit 中按编程语言应用不同的
格式化工具，并在项目中补齐相应格式化配置，保证提交后代码格式统一；同时希望
评估是否需要补充代码风格检查（lint）能力——结论是有必要，一并纳入。

## 变更内容
- .pre-commit-config.yaml（新增）：基于 pre-commit.com 框架，全部使用
  `repo: local` 本地 hook（规避企业 TLS 代理拉取远程仓库失败）。覆盖：
  - Python：`ruff format` + `ruff check --fix`
  - Go：`gofmt -w -s`
  - 前端/JSON/CSS/MD/YAML：`prettier`（缺失时优雅跳过）
  - Proto：`buf format`（缺失时优雅跳过）
  - 通用：去行尾空白 + 末尾换行（scripts/hooks/fix_whitespace.py）
- ruff.toml（新增）：根级 Python 格式化/lint 配置（line-length=100，
  select=E/F/I/UP/B/SIM），统一此前仅 py-common 才有的规则。
- .prettierrc.json / .prettierignore（新增）：前端格式化规则与忽略项。
- .golangci.yml（新增）：Go 风格检查配置，由 `make go-lint` / CI 权威执行。
- scripts/hooks/ 下三个本地 hook 包装脚本：buf/prettier 缺失时 exit 0 不阻塞。
- Makefile：新增 format / format-check / precommit-install 等聚合目标；
  py-lint/py-format 扩展覆盖 scripts/ 目录。
- AGENTS.md：新增「代码格式化与风格检查（强制）」章节。
- 验证：`pre-commit run --all-files` 全绿。

## 设计取舍
- 提交钩子只跑「快且可自动修复」的格式化 + 轻量 lint；重型 lint 放 make/CI。
- 全本地 hook：企业 TLS 代理会拦截远程 hook 仓库与 buf/go 下载，local 最稳。
