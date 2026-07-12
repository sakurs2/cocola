# refactor: 删除旧运行路径并统一 checkpoint 责任

- 变更时间：2026-07-12 17:19 (+08:00)

## 变更理由

核心服务已经固定使用 durable chat、Route A、PostgreSQL、Redis 和 OpenSandbox，
但代码仍保留请求绑定 chat、生产 EchoProvider、进程内持久化 fallback、单实现
Provider 注册表和不可达启动脚本分支。多套路径增加维护成本，并可能让服务在依赖
缺失时以半可用状态启动。显式删除对话时 Agent Runtime 与 Sandbox Manager 还会对
同一 sandbox 重复上传 checkpoint，随后删除 session_map 指针，产生无用 snapshot。

## 变更内容

- `apps/gateway`：删除 `chatLegacy` 及其专用持久化/trace 逻辑；生产要求 PostgreSQL
  Chat Run Store；测试统一使用内存 Run Store；内部 trace 直接挂到 conversation root。
- `apps/agent-runtime`：生产固定使用 Route A，删除 `COCOLA_AGENT_MODE`；移除宿主侧
  `claude-agent-sdk` 和冗余直接依赖；CheckpointManager 收敛为只恢复，SessionMap
  只读取 sandbox-manager 写入的 checkpoint 元数据。
- `apps/sandbox-manager`：Redis 重试失败后启动失败；OpenSandbox 由 composition root
  直接构造；显式 conversation release 不再 checkpoint，停机和 idle reclaim 继续由
  `CheckpointAllActive` / reaper 保存 session。
- `apps/admin-api`：生产固定使用 PostgreSQL + Redis，删除缺依赖时的进程内 Store、
  event broker 与禁用发布分支；scheduler loop 始终启动，`scheduler.enabled` 成为唯一
  可热加载的暂停/恢复控制；清理未使用 Go module 依赖。
- `scripts/run-stack.sh`：删除常量 `DEV_STACK`、不可达 MinIO 二次启动和
  `COCOLA_SKIP_MINIO`；删除已失效的 Echo MVP 脚本。
- `apps/llm-gateway`：生产固定使用 Postgres ledger/registry/trace 与 Redis quota、
  override、revocation，删除 Redis-only ledger 和进程内生产回退；正式入口补齐 token
  revocation；移除客户端 identity header 与 usage query 的兼容覆盖。
- `apps/admin-api`、`apps/web`：管理端统一使用 conversation run / trace span API，删除
  `audit-events`、`traces` 包装接口；26 份 Web Admin 代理实现收敛到一个认证代理 helper。
- `scripts`：删除与 LLM Gateway 单元测试重复的 hermetic tool-use 脚本，保留真实模型
  probe。
- README、配置文档和 compose 注释同步为唯一生产路径。

## 关键取舍

- Warm Pool 的 enabled/size 是容量运行配置，不是灰度开关，继续保留并支持热更新。
- 内存 Store 和静态 Binder 继续作为测试替身存在；EchoProvider 连同生产选择开关删除。
- LLM Gateway 的 Memory ledger/quota 继续作为显式测试替身，生产 composition root
  不再自动选择它们。
- 显式删除 conversation 不保存 snapshot；Ctrl+C、SIGTERM 和 idle reclaim 的
  checkpoint 行为保持不变。
