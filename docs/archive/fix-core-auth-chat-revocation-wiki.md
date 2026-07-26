# fix: 修复身份、聊天附件、Token 撤销与 Markdown 数据安全

- 变更时间：2026-07-26 14:38 (+08:00)

## 变更理由

- Auth.js 的 Session update 曾直接采信客户端提交的用户字段，攻击者可伪造 ID、邮箱或角色；Workspace WebSocket 也直接使用 JWT 邮箱签发 runtime token。
- Chat 请求体和附件缺少统一硬限制，Web 代理还会先读取完整文本；大请求可能造成 Gateway 或 Web 进程内存压力，非法 base64 会被静默丢弃。
- Token 在 PostgreSQL 标记撤销后，即使 Redis denylist 写入失败，Admin API 仍返回成功，导致已撤销 Token 在 Gateway 继续有效。
- Tiptap 不能无损保留 frontmatter、原始 HTML、脚注等 Markdown 语法，富文本保存可能损坏原文。

## 变更内容

- `apps/web/auth.ts`、`apps/web/server.mjs`、`apps/web/lib/account-proxy.ts`：引入内部 `authVersion=2`，Session update 仅按已有 JWT 用户 ID 回查可信账号；WebSocket 校验版本并回查当前账号后再签发 runtime token。
- `apps/gateway/internal/httpapi/simple_chat.go`、`apps/web/app/api/chat/route.ts`、`apps/web/app/runtime-provider.tsx`：限制 Chat 请求体为 48 MiB、每轮最多 8 个附件、单文件及解码后总量最多 32 MiB；Gateway 在创建 Run 前一次性解码校验，Web 使用请求流转发，前端发送前执行同样的数量和大小预检。
- `apps/admin-api/internal/store/mirror.go`、`apps/admin-api/cmd/admin-api/main.go`：Redis 撤销写入失败时同步返回错误；启动时从 PostgreSQL 重放全部已撤销 Token，重放失败则拒绝启动。
- `apps/web/components/wiki/wiki-markdown-editor.tsx`：打开 Markdown 时做一次标准化往返检查；可无损保留时使用 Tiptap，不兼容时固定降级到源码 textarea，仍沿用显式 Save 和现有 revision/ETag 冲突保护。
- 新增并更新 Go 与 Web 测试，覆盖旧 Session 失效、WebSocket 可信账号回查、请求和附件边界、撤销重试与启动重放、Markdown 富文本/源码模式选择。
- 本次明确不修改 Git Skill 主机文件读取链路，也未引入自动保存、Outbox 或后台对账任务。
