# 对话持久化 + 历史渲染

状态:草案(待实现) · 日期:2026-07-02 · 关联:web-new-conversation-session-lifecycle.md、m7-persistence-postgres.md、ADR-0009(Route A)

## 1. 目标

1. **对话持久化**:用户的对话落库;打开侧边栏能看到自己的各个对话列表(倒序)。暂不支持删除。
2. **历史渲染**:点击某个对话,能把该对话的历史消息完整重新渲染出来(文本 / 思考 / 工具调用+结果)。

非目标(本期不做):删除对话、重命名、跨设备实时同步、搜索、分页(先一次性拉取)、多租户共享。

## 2. 已确认决策

- **消息来源 = 路线 A**:gateway 边转发 SSE 边持久化「UI 消息副本」。存的就是前端 `convertMessage` 能直接消费的 parts,点开即渲染,零二次解析。sandbox 内 claude 的 `~/.claude/projects/.../<uuid>.jsonl` 仍是 agent 侧 resume 的真相,与本副本职责分离(接受轻微冗余)。
- **写路径 = gateway**:gateway 是唯一同时握有「已验证 user_id」+「完整 SSE 事件流」的节点。它当前零 PG 接线,需新增,照 admin-api 的 `goose + pgxpool` + `COCOLA_PG_DSN` 门控模式装配。
- **标题 = MVP 截断**:取首条用户消息前 N 字(建议 60)作为标题;LLM 摘要标题后置,不在本期。

## 3. 现状锚点(代码事实)

- `db/embed.go`:`db/migrations/*.sql` 是全库唯一 schema 源;Go 服务 goose 应用,Python 服务共用同库不重定义。目前只有 `00001_init_schema.sql`,**无 conversations/messages 表**。
- `apps/gateway/cmd/gateway/main.go`:env 驱动装配,**无任何 PG 代码**。附件对象存储用 `objstore.ConfigFromEnv()` 门控,可作为「可选依赖注入」的范式。
- `apps/gateway/internal/httpapi/api.go`:`chat()` handler 已 `auth.IdentityOf(r)` 拿 `id.UserID`;逐帧 `writeSSE(w, flusher, ev)`。`chatRequest{Prompt, SessionID, ...}`。注释明确 user_id 来自验证身份而非 body(防冒充)。
- `apps/web/app/runtime-provider.tsx`:`useState<UiMessage[]>` 纯内存;`UiPart` = `{type:"text"|"reasoning", text}` 或 `UiToolCall{type:"tool-call", toolCallId, toolName, argsText, result?, isError?}`;`convertMessage` 已把 `UiMessage` 映射为 assistant-ui `ThreadMessageLike`;`sessionId` 每次 New Chat 轮换随机 id(修 history-bleed 时改的);`onNew` body 发 `{prompt, session_id, attachments?}`。
- `apps/web/app/api/chat/route.ts`:同源 SSE 代理,转发 `${COCOLA_GATEWAY_URL}/v1/chat`,verbatim 透传 authorization。
- `apps/admin-api/internal/store/`:可复用范式 —— `postgres.go`(`pgxpool.New`+`Ping`)、`migrate.go`(`sql.Open("pgx")` + `goose.SetBaseFS(cocoladb.Migrations)` + `goose.UpContext`)、Store 接口 + Memory/Postgres 双实现,`COCOLA_PG_DSN` 门控。

## 4. 数据模型(新增迁移 `db/migrations/00002_conversations.sql`)

沿用 goose 格式(`-- +goose Up/Down` + `StatementBegin/End`),表名归入 gateway 域,`00001` 头注释里补一行说明 gateway 新增 conversations/messages。

```sql
-- +goose Up
-- +goose StatementBegin
-- gateway domain: conversation persistence (route A UI-message mirror).

CREATE TABLE IF NOT EXISTS conversations (
    id          TEXT PRIMARY KEY,            -- 复用前端 session_id
    user_id     TEXT NOT NULL DEFAULT '',    -- 来自验证身份;列表按此过滤
    tenant_id   TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',    -- MVP:首条用户消息截断
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL         -- 侧边栏倒序键
);
CREATE INDEX IF NOT EXISTS idx_conversations_user_updated
    ON conversations (user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS messages (
    id               TEXT PRIMARY KEY,
    conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role             TEXT NOT NULL,          -- 'user' | 'assistant'
    parts_json       JSONB NOT NULL,         -- UiPart[] 原样(text/reasoning/tool-call)
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_conv_created
    ON messages (conversation_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
-- +goose StatementEnd
```

`parts_json` 直接存前端 `UiPart[]` 的 JSON —— 读回后原样进 `convertMessage`,前后端零 schema 漂移。

## 5. gateway 落库(路线 A 写路径)

### 5.1 新增 store 包 `apps/gateway/internal/convo/`

照 admin-api 范式:
- `store.go`:`Store` 接口 + 领域类型(`Conversation`, `Message`, `MessagePart`)。
- `postgres.go`:`pgxpool` 实现;`NewPostgres(ctx, dsn)` 建池 + Ping。
- `migrate.go`:复用 `cocoladb.Migrations` + goose(注意幂等,与 admin-api 同时跑无冲突,goose 版本表去重)。
- 接口方法:
  - `UpsertConversation(ctx, Conversation) error`(INSERT ... ON CONFLICT (id) DO UPDATE SET updated_at,title 仅首次设置)
  - `InsertMessage(ctx, Message) error`
  - `ListConversations(ctx, userID string) ([]Conversation, error)`(WHERE user_id ORDER BY updated_at DESC)
  - `GetMessages(ctx, convID, userID string) ([]Message, error)`(**JOIN 校验 conversation.user_id == userID**,防越权读)

### 5.2 装配(main.go)

新增门控,模仿 objstore 段:
```
if dsn := os.Getenv("COCOLA_PG_DSN"); dsn != "" {
    if err := convo.Migrate(ctx, dsn); err != nil { log.Fatal(...) }
    cs, err := convo.NewPostgres(ctx, dsn)
    if err != nil { log.Warn("conversation persistence disabled: "+err) }
    else { api = api.WithConvoStore(cs); defer cs.Close() }
}
```
**未配置 DSN → 不持久化,退化为当前行为**(侧边栏列表为空,聊天照常)。保持 dev 可跑、零破坏。

> 迁移归属:full stack 里 admin-api 先 boot 已 apply 全部迁移(含 00002)。gateway 的 `convo.Migrate` 幂等,单跑 gateway+PG 时也能自建表。

### 5.3 chat() handler 旁路落库

在现有 `chat()` 中,当 `a.convo != nil`:
1. **进入即** `UpsertConversation{id:req.SessionID, user_id:id.UserID, tenant_id:id.TenantID, title:truncate(req.Prompt,60), created_at/updated_at:now}`(title 仅在 conflict 时不覆盖;updated_at 每轮刷新)。
2. **立刻** `InsertMessage{role:"user", parts_json:[{type:"text",text:req.Prompt}]}`。(附件本期不进 parts,后续可加 file part。)
3. 流式期间在现有 `writeSSE` 回调旁,用一个本地 `reducer` 把事件聚合成 assistant 的 `UiPart[]`(**复刻前端 `reducePart` 语义**:text/thinking 追加、tool_use 追加 tool-call、tool_result 按 tool_use_id 回填)。**注意:sandbox 事件不进消息体**(与前端一致)。
4. **流结束**(`Stream` 返回、无论成功/错误)→ `InsertMessage{role:"assistant", parts_json:aggregated}`;再 `UpsertConversation` 刷新 updated_at。落库失败只 `log.Warn`,绝不影响已 flush 给用户的 SSE(用户体验优先)。

> 关键:落库是**旁路**,永不阻塞/破坏 SSE 主流。任何 PG 错误都降级为日志。

## 6. gateway 读端点

在 `Handler()` mux 注册,均走 `verifier.Middleware`(拿验证身份):
- `GET /v1/conversations` → `ListConversations(id.UserID)` → `[{id,title,updated_at}]`。
- `GET /v1/conversations/{id}/messages` → `GetMessages(id, id.UserID)`;归属不符返回 404/403。返回 `[{id,role,parts}]`,parts 即 `UiPart[]`。
- store 未配置时返回空列表 `[]`(不 500),前端优雅显示无历史。

## 7. web 侧

### 7.1 新增同源代理(照 `api/chat/route.ts`)
- `apps/web/app/api/conversations/route.ts` → GET 转发 `${GATEWAY_URL}/v1/conversations`,透传 authorization。
- `apps/web/app/api/conversations/[id]/messages/route.ts` → GET 转发 messages。
(普通 JSON,不需 SSE 那套 duplex。)

### 7.2 runtime-provider.tsx
- 暴露 `loadConversation(convId)`:fetch messages → 映射回 `UiMessage[]`(role + parts 原样)→ `setMessages(...)` + `setSessionId(convId)`。后续在该对话继续发消息时,`session_id=convId`,gateway 端 UpsertConversation 命中同一行 → 续写,claude 也能 resume(session_map 命中)。
- 暴露 `reloadConversationList()` 或在 `onNew` 的 `done`/finally 后触发列表刷新(让新对话/新标题冒泡到侧边栏)。
- `newConversation()` 现有逻辑不变(轮换 session_id、清空);下次发消息时 gateway 自动建新 conversation 行。

### 7.3 app-sidebar.tsx
- 挂载时 `GET /api/conversations` 渲染列表(title,点击项)。
- 点击 → `loadConversation(id)`,ExternalStore 立即重渲染历史。
- 空列表 / 未登录 / store 未配置 → 显示空态,不报错。

## 8. 边界与一致性

- **越权**:messages 读接口必须 JOIN 校验 user_id;list 只按验证身份的 user_id 过滤。body 里绝不接受 user_id(延续 api.go 现有防冒充约定)。
- **parts 契约**:后端聚合的 `UiPart[]` JSON 结构须与前端 `runtime-provider.tsx` 的 `UiPart` 完全一致(type 取值:`text`/`reasoning`/`tool-call`;tool-call 字段 `toolCallId/toolName/argsText/result?/isError?`)。任一端改字段都要同步。
- **附件**:本期 user 消息 parts 只存文本;附件 file part 后续再加(不阻塞主目标)。
- **中断/错误**:被 abort 或流错误时仍落已聚合的 assistant parts(可能不完整),保证历史可见;不追求完美完整性。

## 9. 落地顺序(供拆 Task)

1. `db/migrations/00002_conversations.sql`(+ 更新 00001 头注释一行)。
2. `apps/gateway/internal/convo/`:store 接口 + Postgres 实现 + migrate + 单测(Memory fake 供 handler 测试)。
3. main.go 门控装配(`COCOLA_PG_DSN`,未配置退化)。
4. `chat()` 旁路落库(user upsert+insert;assistant 聚合 insert);补 handler 单测。
5. 两个读端点 + 归属校验 + 单测。
6. web:两个 `/api/conversations*` 代理 + runtime-provider `loadConversation`/列表刷新 + 侧边栏列表与点击加载。
7. 端到端验收(用户跑全栈):发两轮 → 刷新页面 → 侧边栏见对话 → 点开重渲染 → New Chat 起新对话不串历史。

## 10. 验证门

- Go:`go build ./... && go vet ./... && go test ./...`(gateway 模块;PG 相关测试用 `COCOLA_TEST_PG_DSN` 门控,未配置则跳过,照 admin-api parity_test 范式)。
- web:`tsc --noEmit && lint && build` 全绿。
- 每次提交带 `docs/archive/*` changelog;不 `--no-verify`;不提交 `.claude/`。
