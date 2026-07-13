# ci: 在镜像构建前校验发布版本

- 变更时间：2026-07-13 11:02 (+08:00)

## 变更理由

Release workflow 原先会响应任意 `v*` tag 并立即启动七组多架构镜像构建，无法提前
阻止 `v2.0`、版本回退、乱序预发布或已存在 Release 的重复发布。错误通常到镜像
metadata 或 GoReleaser 阶段才暴露，浪费构建资源，也可能留下部分镜像已经推送的
中间态。

## 变更内容

- `.github/workflows/release.yml`：新增前置 `validate` job；所有镜像构建依赖该 job，
  并通过 GitHub API 拒绝已经存在的同名 Release。
- `scripts/validate_release_version.py`：使用 Python 标准库校验规范 SemVer、正式版本
  递增、下一版本预发布及同版本预发布顺序，不增加第三方依赖。
- `scripts/tests/test_validate_release_version.py`：覆盖重大版本跳跃、回退、非法格式、
  预发布排序和历史非规范 tag。
- `docs/cli.md`、`docs/adr/0021-unified-cli-and-bootstrap-installer.md`：记录发布 tag 约束。

## 关键取舍

旧的非 SemVer tag 不参与排序，避免历史脏 tag 永久阻塞发布；新发布必须使用完整
`vMAJOR.MINOR.PATCH`。同名 Git tag 是本次 workflow 的正常输入，是否属于重复发布以
GitHub Release 是否已经存在为准。
