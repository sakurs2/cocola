# Feat: 对话持久化 + 历史渲染(Route A / gateway 落库 / MVP 标题)

## 目标
1. 用户对话持久化存储,打开侧边栏可看到各个对话(暂不支持删除)。
2. 点击对话后,历史对话内容能被完整渲染出来。

## 方案(已确认)
- **路线 A(UI-message 镜像)**:网关把 SSE 事件流聚合成前端渲染所需的 `[]Part` 直接落库;沙箱磁盘上的 claude JSONL 仍是 `--resume` 的唯一真源,两者刻意分离(接受轻微冗余)。
- **写路径落在 gateway**:对话旁路持久化,任何写失败都 `log.Warn` 吞掉,绝不打断 SSE 流。
- **conversation.id == 前端 session_id**(1:1,无额外映射)。同一 session 的后续回合更新同一 conversation 行,并让 agent 通过 session_map `--resume`。
- **MVP 标题**:取首条用户消息首行,rune 截断到 60 + "…"。

## 数据模型
`db/migrations/00002_conversations.sql`(goose,经 `db/embed.go` 内嵌;未回改 00001):
- `conversations(id PK, user_id, tenant_id, title, created_at, updated_at)` + `idx(user_id, updated_at DESC)`。
- `messages(id PK, conversation_id FK ON DELETE CASCADE, role, parts_json JSONB, created_at)` + `idx(conversation_id, created_at)`。

## 改动

### gateway convo store 包(新增 `apps/gateway/internal/convo/`)
- `store.go`:`Store` 接口 + 类型。`Part{Type,Text,ToolCallID,ToolName,ArgsText,Result *string,IsError}`,json tag 与前端 `UiPart` 逐字对齐(`toolCallId/toolName/argsText/result/isError`,均 omitempty,Result 为指针)。常量 `text/reasoning/tool-call`。
- `memory.go`:内存实现(读写锁)。`UpsertConversation` 对已存在 id 只刷新 `updated_at`(标题保留);`ListConversations` 按 user 过滤、`updated_at DESC` 排序;`GetMessages` 归属不符或不存在返回 `ErrNotFound`。
- `postgres.go`:pgxpool 实现。`UpsertConversation` 用 `ON CONFLICT (id) DO UPDATE SET updated_at=EXCLUDED.updated_at`(标题永不覆盖);`GetMessages` 先查归属再取消息 `ORDER BY created_at ASC`。
- `migrate.go`:`Migrate(ctx,dsn)` = `sql.Open("pgx")` + `goose.SetBaseFS(cocoladb.Migrations)` + `UpContext`。
- `reducer.go`:`Reducer` 精确镜像前端 `runtime-provider.tsx` 的 `reducePart/appendTo/fillToolResult`:text/thinking 合并到尾部同类 part;tool_use 追加 tool-call;tool_result 按 id 回填(未匹配则降级为文本);error 追加告警文本。
- 单测 `convo_test.go / reducer_test.go / parity_test.go`(PG parity 由 `COCOLA_TEST_PG_DSN` 门控)。

### gateway 装配与端点
- `main.go`:`COCOLA_PG_DSN` 门控——未设即内存态/特性暗置;已设则 `Migrate` + `NewPostgres`,失败降级为 `log.Warn` 且不启用持久化。
- `internal/httpapi/api.go`:
  - `WithConvoStore(store)`;`chat()` 旁路——有 `session_id` 时 `persistUserTurn`(upsert 会话+落 user 消息),流中喂 `reducer.Apply`,流结束用 `context.Background()` `persistAssistantTurn`(客户端断连不影响落库)。
  - 新增读端点 `GET /v1/conversations`、`GET /v1/conversations/{id}/messages`,均走 `verifier.Middleware`;`user_id` 恒取自校验身份(反冒充),`ErrNotFound` → 404。
  - `internal/httpapi/convo_test.go`:持久化/无 session 跳过/无 store 空列表/归属不符 404,全绿。

### web
- `app/api/conversations/route.ts`、`app/api/conversations/[id]/messages/route.ts`:同源代理到网关,原样透传 `authorization`,`nodejs`/`force-dynamic`,上游状态码穿透(保留 404),异常 → 502。
- `app/runtime-provider.tsx`:新增 `ConversationSummary` 类型与 `conversations` 状态;`refreshConversations`(挂载时 + token 变化 + 每回合结束刷新)、`loadConversation`(拉取消息 → 映射回本地 `UiMessage` → 指向该 session,后续回合续接同一对话);context 扩展 4 个字段。
- `components/assistant-ui/app-sidebar.tsx`:静态 Chats 改为动态列表,空态 "No conversations yet",当前对话高亮。

## 验证
- gateway:`GOWORK=off go vet ./...` 全绿;`go test ./internal/convo/... ./internal/httpapi/...` 全绿。
- web:`tsc --noEmit` 全绿;`next lint` 无告警;`next build` 成功,`/api/conversations`、`/api/conversations/[id]/messages` 均已注册。

## 边界(本次未做)
- 不支持删除对话。
- 标题为 MVP 截断,无智能摘要。
- 沙箱 JSONL 与镜像库刻意分离,不做双向对账。

## 关联
- Plan: docs/plan/conversation-persistence-history-rendering.md
