# feat: 节点本地 Session 持久化

- 变更时间：2026-07-16 11:49 (+08:00)

## 变更理由

复杂 Agent 任务会下载依赖并产生大量中间文件，Sandbox 空闲回收后需要保留
Workspace 和 Runtime 状态。同时 cocola 面向小型团队，不应为单副本数据引入
Longhorn、快照 Controller、后台存储状态机和周期 reconcile。多节点下必须明确保证
恢复到原存储节点；原节点不可用时不能静默创建空目录。

## 变更内容

- `db/migrations/00039_local_session_storage.sql`：破坏性移除 checkpoint 状态，新增精简的 Session、PVC、节点和 generation 绑定表。
- `apps/sandbox-manager`：按请求创建 local-path PVC，恢复时固定原节点，用户确认后才跨节点 reset；回收只销毁 Sandbox。
- `apps/agent-runtime`：移除 checkpoint 恢复，透传 Workspace reset 状态，并将 Claude/Codex Skill 统一为 digest 驱动的持久 Skill Set。
- `apps/gateway`、`packages/proto`、`apps/web`：贯通 reset 协议；Conversation 删除提交后释放 Run 全局锁，再做有界的 Workspace 清理；展示节点不可用确认与 reset 提示。
- `apps/admin-api`、Admin 页面：Nodes 展示 Ready/Schedulable/DiskPressure 和节点影响面；独立 Storage Tab 展示节点物理剩余空间、Session Storage 列表、按需实际占用测量和安全的孤儿 PVC 删除入口。
- `deploy/opensandbox-k8s`、`deploy/sandbox-runtime`、`scripts`：增加 `cocola-local-session` StorageClass，统一挂载 `/session`，将 `/cache` 保持为临时目录，并配置 `/var/lib/cocola/storage`。
- `apps/cli`、`.env.example`：默认 Session 请求容量改为可选的 `2Gi`；legacy Docker 部署使用宿主机 Session 目录且不再配置 checkpoint/Warm Pool。
- `docs/adr/0023-node-local-session-storage.md` 及当前使用文档：记录单副本、本地盘、显式 reset、无后台存储循环和 MinIO 职责边界。
- Review 修复：Acquire/Release 统一使用 Session 锁并以 Redis CAS fencing；Redis 运行绑定记录 PVC/generation 并在复用前与 PostgreSQL 核对；Sandbox 必须先销毁再解绑或删除候选 PVC；Shared Skill bundle 写入原子切换的 Session Skill Set；Admin 按 generation 保护当前和在途 PVC，节点下线只 cordon 而不执行无效 Pod Eviction。
- 存储可见性：新增只读 `cocola-storage-probe` DaemonSet，通过 Kubernetes Pod Proxy 在页面刷新时读取 `statfs`；Session 实际占用仅在管理员点击 Measure 后遍历单个已校验 PVC，不提供文件浏览、内容读取或后台扫描。

关键取舍：`2Gi` 是 local-path 的软请求而非硬配额；不提供副本、备份、自动迁移、回滚或存储定时任务，节点磁盘损坏风险由运维明确承担。
