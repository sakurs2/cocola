# fix: 修复 Dependabot 安全升级与 PR Buf 基线检查

- 变更时间：2026-07-27 16:42 (+08:00)

## 变更理由

Dependabot 首次运行后暴露了三类确定性失败：

- pnpm workspace 的 Next.js 自动安全更新从 `apps/web` 子目录执行，无法更新根锁文件；
- 多个传递依赖受安全公告影响，但旧锁文件无法解析到安全版本；
- `@assistant-ui/react` 使用本地 patch，Dependabot 升级版本时无法自动迁移 patch。

同时，所有 Dependabot PR 的 `proto / buf` 检查都在失败。PR checkout 只有
`origin/master` 远端跟踪引用，Buf 从本地 `.git` clone `master` 分支时找不到该引用。

## 变更内容

- `apps/web/package.json`、`pnpm-lock.yaml`：升级 Next.js 与匹配的 ESLint 配置到
  15.5 安全版本，并更新受影响的传递依赖。
- `package.json`：为仍由上游精确锁定的漏洞依赖增加同大版本安全 overrides。
- `apps/web/app/**`：适配 Next.js 15 的异步动态路由参数与查询参数类型。
- `apps/web/components/assistant-ui/session-status-panel.tsx`：使用 Next.js `Link`
  完成站内 MCP 设置跳转。
- `.github/dependabot.yml`：忽略无法自动迁移 patch 的 `@assistant-ui/react`；
  该依赖后续升级必须人工重放并验证 patch。
- `.github/workflows/ci.yml`：PR 中将基线提交 checkout 到独立目录，再由
  `buf breaking` 比较该目录，避免依赖本地分支引用。
- 验证：pnpm frozen-lockfile 安装通过；Web 格式检查、ESLint、119 个 Node
  单测和 Next.js 生产构建通过。
