# chore: 变更日志目录从 tmp/dev 迁至 docs/archive

- 变更时间：2026-06-11 11:28 (+08:00)

## 变更理由

`tmp/dev/` 名义是"临时"目录,却承载了随仓库提交的人类可读变更日志,语义自相
矛盾,且需要在 .gitignore 里写 `tmp/*` + 例外才能既忽略 tmp 又保留该目录。
把变更日志收敛到 `docs/archive/`(普通受控目录)后,语义清晰、无需 gitignore
例外,且和 docs/adr、docs/api 等文档同处一棵树。

## 变更内容

- tmp/dev/*.md → docs/archive/(git mv,保留历史),共 9 个变更记录文件。
- 删除空的 tmp/dev/ 目录。
- .gitignore:移除 `tmp/*` + 该目录例外的组合,改为 `tmp/` 整体忽略
  (tmp 回归纯 scratch 用途,永不提交)。
- AGENTS.md:全部 `tmp/dev/` 引用改为 `docs/archive/`(3 处);并把"已纳入版本
  控制(见 .gitignore 例外)"措辞改为"普通的版本控制目录",不再依赖例外。
- 约定变更:后续所有变更记录一律新增到 docs/archive/,不再写入 tmp/dev/。
