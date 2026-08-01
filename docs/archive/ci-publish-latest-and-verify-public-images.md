# ci: 发布 latest 并校验镜像公开可用

- 变更时间：2026-08-02 00:04 (+08:00)

## 变更理由

源码构建的开发版 CLI 默认使用 `latest` 镜像，但 Release workflow 没有显式声明该标签的
发布策略。首次公开版本前 GHCR 中也只有 Sandbox Runtime，其他应用镜像尚未创建；即使镜像
构建成功，新建 Package 的默认私有可见性仍可能让匿名生产安装收到 `denied`。

## 变更内容

- `.github/workflows/release.yml`：稳定 SemVer 发布显式追加 `latest`，预发布版本不覆盖稳定
  通道；每个多架构镜像推送后退出 GHCR 登录并重试匿名 manifest 查询，全部公开可读后才允许
  GoReleaser 发布 CLI。
- `scripts/tests/test_release_workflow.py`：静态验证 `latest` 策略和匿名镜像发布门禁不会被后续
  修改意外删除。
- `docs/cli.md`：说明开发版 CLI 的 `latest` 通道、预发布隔离和首次 Package 公开要求。

## 关键取舍

- 保留开发版 CLI 默认使用 `latest` 的简洁体验，但只允许正式版本更新该可变标签。
- GHCR Package 可见性仍由维护者在 GitHub 设置为 Public；workflow 不保存额外 PAT，也不尝试
  绕过 GitHub 的人工可见性确认，而是在 CLI Release 前以匿名读取结果作为硬门禁。
