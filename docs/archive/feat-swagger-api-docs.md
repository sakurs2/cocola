# feat: Swagger API 文档页

- 变更时间：2026-07-07 15:44 (+08:00)

## 变更理由

项目需要一套完整、可在页面中打开的 API 文档，便于管理员和开发者查看 Gateway、Admin API、LLM Gateway 的当前 HTTP 接口。

## 变更内容

- `docs/api/`：新增 Gateway、Admin API、LLM Gateway 三份 OpenAPI 3 静态规范。
- `apps/web/app/admin/api-docs/`：新增管理员 Swagger API 文档页，支持在三份服务文档间切换。
- `apps/web/app/api/admin/api-docs/[spec]/route.ts`：新增管理员鉴权保护的 OpenAPI 规范读取接口，并限制只能读取白名单规范文件。
- `apps/web/components/admin/admin-shell.tsx`：在管理员导航 Overview 中新增 API Docs 入口。
- `apps/web/package.json`、`pnpm-lock.yaml`：新增 Swagger UI 渲染依赖。
