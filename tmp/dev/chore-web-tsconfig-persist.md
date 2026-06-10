# chore: 固化 Next.js 生成的 tsconfig

- 变更时间：2026-06-10 13:25 (+08:00)
- 关联提交：140e85d

## 变更理由
Next.js dev server 每次启动都会自动向 apps/web/tsconfig.json 写入
`allowJs` / `noEmit` 并重新格式化，导致工作区反复变脏。

## 变更内容
- apps/web/tsconfig.json：提交其自动生成后的形态，避免后续启动再次改写。
