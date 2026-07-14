# ci: 升级 GitHub Actions Node.js 运行时

- 变更时间：2026-07-14 15:47 (+08:00)

## 变更理由

GitHub Actions 已弃用 Node.js 20 Action 运行时。现有 CI、发布和 Sandbox 镜像
workflow 引用了多项仍声明 `node20` 的旧版 Action，执行时会被 GitHub 强制切换到
Node.js 24 并产生兼容性警告。

## 变更内容

- `.github/workflows/ci.yml`：升级 checkout、Go、Python、Node 和 pnpm Action；使用
  Buf 的统一 Action 以 setup-only 模式安装 CLI；Web 构建 Node.js 版本与 Dockerfile
  对齐为 22。
- `.github/workflows/release.yml`：升级 GitHub Script、Docker、GoReleaser 等 Action
  到使用 Node.js 24 的主版本。
- `.github/workflows/proto-breaking.yml`：切换到 Buf 统一 Action 的 setup-only 模式。
- `.github/workflows/sandbox-runtime-image.yml`：升级 Docker 构建与发布 Action。
- `golangci-lint-action` 的 Node.js 24 版本只支持 golangci-lint v2；为避免本次夹带
  lint 配置迁移，CI 改为直接安装并执行原有 v1.59.1，保持现有规则语义。
