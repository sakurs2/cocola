# fix: 按语义校验 Forgejo 源码 Release 元数据

- Change time: 2026-08-10 18:11 (+08:00)

## Reason

`v0.1.20` 的全部 Cocola 镜像已成功构建和推广，但 Forgejo 源码归档 job 仍失败。工作流创建的草稿
Release 已包含正确的标题、上游地址、版本、commit、镜像 digest 和源码 SHA；根因是代码要求
GitHub API 返回的 Markdown 正文与本地文件逐字节相等，平台对正文格式的规范化造成了误判。

## Changes

- `.github/workflows/release.yml`：保留源码 Release 标题的精确校验，将正文校验收敛为上游地址、
  Forgejo 版本、commit、许可证、镜像 digest 和源码 SHA 六项必要语义字段。
- `.github/workflows/release.yml`：继续对源码包和 provenance 资产执行 SHA-256 精确校验，正文格式
  宽容不会降低实际分发内容的完整性保护。
- `scripts/tests/test_release_workflow.py`：增加 Markdown 格式规范化不会触发误判、必要来源字段不可
  缺失的回归约束。

## Tradeoffs

- 人工调整正文排版不再阻断后续 Cocola Release；标题、关键来源字段和二进制资产摘要仍是强约束，
  在可维护性与供应链完整性之间保持清晰边界。
