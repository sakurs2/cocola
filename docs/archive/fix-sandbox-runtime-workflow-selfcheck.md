# fix: sandbox runtime workflow selfcheck

- 变更时间：2026-07-05 23:59 (+08:00)
- 关联提交：待提交

## 变更理由

`sandbox-runtime-image` workflow 已经成功 build/push 镜像，但推送后的 selfcheck
步骤使用 `grep` 配合 `set -euo pipefail` 校验 JSON，所有工具实际可用时仍可能因为
无匹配返回码导致 step 失败，表现为日志里 selfcheck JSON 全绿但 job 失败。

## 变更内容

- .github/workflows/sandbox-runtime-image.yml：把 selfcheck 校验从 shell `grep`
  改为 Python JSON 解析，显式检查工具字段是否以 `missing` 或 `error:` 开头。

## 关键取舍 / 注意事项

- 只改 CI 校验方式，不改变镜像构建、tag 发布或运行时内容。
- Python 是 GitHub hosted runner 的基础能力，避免额外依赖。
