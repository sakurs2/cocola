# feat: 系统定时任务初版

- 变更时间：2026-07-05 16:50 (+08:00)

## 变更理由

管理员需要在 admin 页面创建系统级定时任务，配置任务名、调度方式、prompt、模型和附件，并能监控任务运行状态；用户侧也需要能在 Chat 入口创建自己的定时任务，并在首次运行完成后像普通对话一样进入 chat history。

## 变更内容

- `db/migrations/00012_scheduled_tasks.sql`：新增系统任务、附件、运行记录和事件表，任务配置预留 `config_json` 扩展字段。
- `apps/admin-api`：新增 scheduled task store/service/http 能力，包含 1 小时最小调度频率校验、任务启停、手动排队和内置 worker。
- `apps/admin-api`：新增 `/me/scheduled-tasks` 用户任务接口；系统任务继续直连 agent-runtime，用户任务通过 gateway `/v1/chat` 运行以复用 chat history。
- `apps/gateway`：`/v1/chat` 支持后台任务传入对话标题和“完成后再显示”的隐藏会话模式。
- `apps/web`：新增 `/admin/scheduled-tasks` 页面、admin 代理路由和 Chat 侧边栏 Schedule 入口，支持模型选择、prompt、附件上传、运行看板和运行详情抽屉；用户侧 Schedule 弹窗支持查看、编辑、暂停、恢复、删除自己的任务。
- `apps/web`：Chat history 会根据 `chat_type` 在标题前展示类型图标，当前区分普通对话和定时任务对话，并为后续新类型预留扩展入口。
- `deploy/docker-compose/docker-compose.full.yml`：给 admin-api 注入 `COCOLA_AGENT_ADDR` 和 `COCOLA_GATEWAY_URL`，让容器化 worker 可访问 agent-runtime 与 gateway。
- `.env.example`：补充系统定时任务 worker 与 1 小时最小间隔配置说明。
- `db/migrations/00013_scheduled_tasks_default_max_turns.sql`：把系统任务默认 `max_turns` 从 1 提高到后端固定的 30，并修复初版已创建的低预算任务，避免工具调用任务报 “Reached maximum number of turns (1)”。
- `db/migrations/00014_user_scheduled_tasks_chat_history.sql`：补充用户任务归属、稳定 conversation id，以及隐藏会话字段。
- `db/migrations/00015_conversation_chat_type.sql`：给 conversations 增加 `chat_type` 字段，定时任务运行产生的会话标记为 `scheduled_task`，普通对话默认为 `chat`。
- `db/migrations/00016_backfill_scheduled_task_chat_type.sql`：回填已存在的用户定时任务会话类型，避免早期测试生成的定时任务会话仍显示为普通 chat 图标。
- `db/migrations/00017_backfill_legacy_user_task_conversation_id.sql`：继续兜底旧用户任务 `conversation_id` 为空的情况，按 `sched-<task_id>` 写回并回填对应历史会话类型。
- 关键取舍：附件初版以不对外返回的 `content_b64` 持久化跑通闭环，同时保留 `object_key` 便于后续切到对象存储。
