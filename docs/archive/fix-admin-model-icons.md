# fix: 修复 Admin 模型品牌图标

- 变更时间：2026-07-13 23:18 (+08:00)

## 变更理由

Admin 模型列表使用 Next.js 图片优化加载动态 SVG 图标，导致 volcengine、chatglm 等品牌图标无法显示；同一图标在用户对话页面使用未优化加载，因此展示正常。

## 变更内容

- `apps/web/app/admin/models/page.tsx`：动态模型图标与用户对话页保持一致，禁用 Next.js 图片优化。
- LLM Gateway 继续透明转发 Responses 请求，不增加供应商定制字段清理。
