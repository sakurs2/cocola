# feat: 接入 OpenViking 用户级长期记忆

- 变更时间：2026-07-20 18:41 (+08:00)

## 变更理由

Cocola 需要默认关闭、可由管理员热配置的用户级长期记忆能力。记忆引擎统一复用 OpenViking，同时保持高内聚、低耦合：业务模块不直接依赖 OpenViking 协议，PostgreSQL 不保存记忆正文或向量，也不重新引入已删除的通用 OpenAI Chat Completions Provider。

## 变更内容

- `db/migrations/00040_user_memory.sql`：新增单例配置、维度锁、用户开关和 Capture 任务表；Provider 约束只增加精确的 `openai_embeddings` 类型。
- `apps/admin-api`、`apps/web/app/admin/toolbox`：新增 Memory Toolbox、乐观锁配置、模型/维度/readiness 校验及 Disabled、Incomplete、Ready、Degraded 状态。
- `apps/llm-gateway`：新增仅支持 `/embeddings` 的 Provider，以及使用独立 service token 的内部 Memory Chat/Embeddings 窄协议适配器；路由选择按请求热加载，使用量计入 `memory-service`。
- `apps/gateway/internal/memory`：集中封装 OpenViking trusted 身份、Recall、Prompt 截断、异步 Capture、有界重试、epoch 清理、用户 CRUD、错误脱敏和指标；其他 Gateway 模块只依赖高层服务接口。
- `packages/proto`、`apps/agent-runtime`：新增 `memory_context`，以低优先级、不可信且可能过时的 system context 注入，不改写原始用户消息。
- `apps/web/components/profile`：新增用户 Memory 设置、查看、单条删除和清空入口；全局关闭时开关只读，已有数据仍可管理。
- `deploy/docker-compose/docker-compose.dev.yml`、`apps/cli/internal/assets/compose.yaml`、`scripts/run-stack.sh`：固定 OpenViking v0.4.10 镜像 digest 和同版本 Go SDK，使用 MinIO `openviking/` 前缀、独立持久卷及 `/ready`，并同步开发与正式部署环境变量。
- Capture 恢复链路：worker 使用可取消根 context 保证 Gateway 快速退出；OpenViking task tracker 使用持久后端；task 丢失或 commit 响应未落库时，按确定性 Session ID 反查 task，并结合 archive `.done`、`.failed.json`、`messages.jsonl` 标记恢复，避免重复提交同一轮对话。
- 对话消息 UI：Recall 以可替换的 `memory-recall` 过程 Part 实时展示；命中时只显示使用数量，部分失败或不可用时显示脱敏提示，未命中/关闭时自动移除。该 Part 随草稿和历史消息持久化并进入 `Processed` 折叠区，但不包含记忆正文、OpenViking URI 或上游错误详情，也不会进入复制内容。
- `.env.example` 与本地 `.env`：同步 OpenViking URL/root key、Memory LLM service token 和 1024 维度配置；全局开关只存 PostgreSQL，不增加环境变量。

关键取舍：OpenViking 是唯一事实正文与向量存储；PostgreSQL 仅保存控制面配置和不含对话正文的任务状态。Recall 失败时继续正常回答，Capture 失败不改变 Agent Run 结果，Scheduled Task 不召回也不学习。
