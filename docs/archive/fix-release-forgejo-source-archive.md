# fix: 按 Forgejo 版本归档对应源码

- Change time: 2026-08-10 16:15 (+08:00)

## Reason

`v0.1.18` Release 在 CLI job 中失败。Forgejo 源码 artifact 被下载到仓库内的
`release-assets/`，GoReleaser 因发现未跟踪文件而以 dirty worktree 拒绝发布。Forgejo
版本不会随每个 Cocola 版本升级，重复下载并附加同一份源码也造成了不必要的网络和存储开销。

## Changes

- `.github/workflows/release.yml`：将 Forgejo 对应源码改为专用、非 Latest 的
  `forgejo-source-v16.0.1` Release；首次创建或资产缺失时才下载到 `$RUNNER_TEMP`，之后仅通过
  GitHub Release Asset digest 校验。
- `.github/workflows/release.yml`：支持草稿发布和缺失资产补齐；已有资产 digest 或 Release
  元数据不一致时拒绝覆盖，并保留匿名下载验证。
- `.github/workflows/release.yml`：移除 CLI job 的源码 artifact 下载和重复上传，保证 GoReleaser
  checkout 始终干净。
- `.goreleaser.yml`：明确忽略 `forgejo-source-*` 辅助 tag，避免同一提交的第三方源码 tag 参与
  Cocola SemVer 当前版本和上一版本选择。
- `scripts/tests/test_release_workflow.py`、`THIRD_PARTY_NOTICES.md`：增加 dirty-worktree 和 tag 隔离
  回归覆盖，并将长期源码入口更新为 Forgejo 版本级专用 Release。

## Tradeoffs

- 专用源码 Release 会在仓库 Release 列表中增加一个明确标注的第三方归档，但避免每个 Cocola
  版本重复保存约 11 MiB 的相同源码。
- 归档 tag 不使用 `v` 前缀且显式设置为非 Latest，因此不会触发 Cocola Release 工作流，也不会
  干扰普通用户获取最新 Cocola 版本。
