# fix: 直接使用 Forgejo 上游镜像

- Change time: 2026-08-10 23:26 (+08:00)

## Reason

Forgejo 镜像同步到 Cocola GHCR 的主要目的，是配合已经移除的中国大陆公共镜像代理预设。
该预设退出后，再分发没有改善完整冷启动路径，却让每次 Cocola Release 都依赖镜像复制、匿名
访问验证和 GPL 对应源码归档。Forgejo 版本长期固定，这条重复执行的链路增加了发布失败面，
但没有持续的用户价值。

## Changes

- `apps/cli/internal/config/images.go`：Forgejo 改为直接使用 Codeberg 上游版本与多架构 Manifest
  digest 固定的镜像引用；自定义 Cocola Registry 不改写第三方 Forgejo 来源。
- `.github/workflows/release.yml`：删除 Forgejo 镜像复制、匿名验证和源码归档 Job，CLI Release
  只等待 Cocola 自有镜像验证与推广。
- `scripts/tests/test_release_workflow.py`：删除再分发实现测试，增加发布工作流不得重新引入 Forgejo
  镜像 Job 的边界测试。
- `THIRD_PARTY_NOTICES.md`、`docs/cli.md`：将 Forgejo 描述收敛为直接上游依赖，不再声明 Cocola
  再分发或对应源码资产。

## Tradeoffs

- 安装 Forgejo 需要访问 Codeberg，但移除公共镜像加速后本就无法通过 Cocola 保证中国大陆镜像
  下载；直接访问权威上游减少了一层供应链和发布依赖。
- 保留 `.goreleaser.yml` 对历史 `forgejo-source-*` 标签的忽略规则，避免已经发布的辅助标签干扰
  后续版本计算；旧镜像和源码 Release 作为历史发布记录保留，不执行破坏性删除。
- 保留旧 `cn-mirror` Schema 迁移和相关测试，确保 v0.1.18 至 v0.1.22 的现有安装能够退出失效代理。
