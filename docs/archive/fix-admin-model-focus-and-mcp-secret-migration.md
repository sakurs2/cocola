# fix: 移除模型表单焦点光圈并迁移历史 MCP 密文

- 变更时间：2026-07-13 22:54 (+08:00)

## 变更理由

Admin Models 的输入框即使声明了 `outline-none`，仍会被 Admin 全局
`focus-visible` 规则覆盖，点击后出现明显的蓝色外圈。

同时，2026-07-13 配置规范化后，开发环境开始为
`COCOLA_CONFIG_SECRET_KEY` 注入独立默认值，并删除了旧的 Model Secret
回退。此前在未显式配置 Config Secret 时创建的 MCP 记录使用 Model Secret
加密；这些历史密文没有同步迁移，导致 Admin MCP 列表和 Agent Runtime 的
MCP catalog 解密失败并返回 500。

## 变更内容

- `apps/web/app/globals.css`：Admin 表单控件聚焦时使用中性边框，不再显示浏览器或 Tailwind 蓝色光圈；按钮和链接仍保留键盘焦点提示。
- `apps/admin-api/internal/service/mcp.go`：新增幂等的历史 MCP 密文迁移，将旧 Model Secret 密文重新加密为当前 Config Secret，并支持部分迁移后的混合记录。
- `apps/admin-api/cmd/admin-api/main.go`：Admin API 接收请求前执行迁移；无法用新旧密钥解密时明确启动失败，避免运行中持续返回模糊的 500。
- `apps/admin-api/internal/service/mcp_test.go`：覆盖旧密文无法直接读取、迁移后恢复以及重复迁移不改写当前密文。

迁移只发生在启动阶段，不在正常请求链路保留双密钥 fallback。
