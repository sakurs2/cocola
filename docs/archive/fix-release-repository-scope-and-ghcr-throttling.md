# fix: 修复源码归档仓库作用域并限制 GHCR 并发

- Change time: 2026-08-10 17:00 (+08:00)

## Reason

`v0.1.19` Release 有两个独立失败。Forgejo 源码归档 job 为减少开销没有 checkout 仓库，但
`gh release` 命令依赖当前 Git 仓库推断目标，因而报出 `fatal: not a git repository`。同时，八个
多架构镜像并行向 GHCR 推送，Gateway 镜像被 GitHub secondary rate limit 以 403 拒绝。

## Changes

- `.github/workflows/release.yml`：为 Forgejo 源码归档中的所有 `gh release create/upload/edit`
  命令显式传入当前 GitHub 仓库，不为一个 API 操作额外 checkout 完整仓库。
- `.github/workflows/release.yml`：将镜像构建与稳定别名推广矩阵的最大并发统一限制为 4，降低
  GHCR 同时写入 manifest、attestation 和 tag 的压力。
- `scripts/tests/test_release_workflow.py`：增加 Release 命令仓库作用域和 GHCR 写入并发上限的
  回归测试。

## Tradeoffs

- 镜像发布耗时会比八路完全并发略长，但不增加自定义重试器或新的 Action 依赖，失败行为仍然
  清晰，且可显著降低触发 GHCR 二级限流的概率。
