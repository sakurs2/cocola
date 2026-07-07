# feat: 用户 Token 用量看板

- 变更时间：2026-07-07 16:33 (+08:00)

## 变更理由

管理员需要诊断与治理 LLM token 消耗，查看指定时间范围内的总用量、趋势、用户排行，并能导出 Excel 进行离线分析。

## 变更内容

- `apps/admin-api`：新增基于 `usage_ledger` 的 token usage 聚合接口、单用户趋势接口和 Excel 导出接口。
- `apps/web`：新增 `/admin/token-usage` 看板，使用 Chart.js 展示全局与单用户 token 趋势，并支持 Excel 下载。
- `docs/api/admin-api.openapi.yaml`：同步新增 token usage API 文档。
- `apps/admin-api/go.mod`、`apps/web/package.json`：新增 Excel 导出与 Chart.js 相关依赖。
