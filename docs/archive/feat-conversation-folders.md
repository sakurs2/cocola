# feat: 用户对话 Folder 分类

- 变更时间：2026-07-14 11:20 (+08:00)

## 变更理由

用户需要在不改变 Agent 上下文、Runtime、MCP 或 Skills 行为的前提下，用私有的扁平 Folder 整理普通对话。Folder 必须与现有对话链路共用同一套 Run 和 Composer，且删除、移动和首轮创建不能产生部分写入或虚假的成功状态。

## 变更内容

- `db/migrations/00037_conversation_folders.sql`：新增用户私有 Folder、Conversation 归属、普通对话约束、大小写不敏感唯一名称及级联删除关系。
- `apps/gateway/internal/convo/`：为 Memory/PostgreSQL Store 增加 Folder CRUD、移动和原子级联删除，并保持所有权与名称语义一致。
- `apps/gateway/internal/chatrun/`：首轮 Run 在同一持久化边界内校验并绑定 Folder；幂等重试复用原 Run，已有对话禁止通过 Chat 请求改变 Folder。
- `apps/gateway/internal/httpapi/`：新增 Folder API、对话移动 API、运行中操作阻断和最多 4 并发/10 秒的有界 Session 清理。
- `apps/web/app/runtime-provider.tsx`：统一维护 Folder 状态和首轮 Folder session 提示，服务端确认 Run 创建前不会丢失归属。
- `apps/web/components/assistant-ui/`、`apps/web/app/folders/[id]/page.tsx`：实现动态侧边栏、Radix 操作菜单与确认框、Folder Composer 页面和 Folder 对话列表。
- `apps/web/components/assistant-ui/workspace-toast.tsx`：增加 Workspace 级居中成功 Toast；对话移动成功后显示目标 Folder，并在 1.8 秒后自动消失。
- Folder 操作菜单显式使用用户侧深色前景 token，避免 Portal 挂载在 dark 根节点下时高亮文字与背景混合。
- Folder 页面以本次新 Conversation 的运行状态作为首轮发送信号，避免 assistant-ui 短暂保留上一对话消息时误跳回 Workspace 首页。
- 侧边栏移除未接入业务能力的静态 Channels 占位入口，仅保留真实可用的导航与 Folder/Chat 数据。
- 测试：覆盖 Folder CRUD、名称唯一、所有权、移动约束、无效 Folder 零写入、运行中阻断、级联删除和 Session 释放；已通过 Gateway 全量 Go tests、PostgreSQL parity、Web TypeScript、lint 与 production build。

## 关键边界

- Folder 仅组织 `chat` 对话，不进入 Agent prompt，也不支持嵌套、描述或定时任务归档。
- Chats 始终展示全部对话；Folder 是额外分类入口。
- 删除 Folder 在数据库提交后再做有界 Session 清理；清理失败不回滚已经提交的用户数据删除。
- Folder 页面首轮发送后进入标准对话页，不复制 SSE 或 Run 执行链路。
