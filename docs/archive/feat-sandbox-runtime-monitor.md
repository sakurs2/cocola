# feat: sandbox runtime monitor

- 变更时间：2026-07-04 19:28 (+08:00)

## 变更理由

管理员已有 Sandbox Nodes 节点调度页面，但缺少沙箱实例视角：无法直接看到当前有哪些沙箱、沙箱绑定到哪个对话、归属用户是谁，以及沙箱处于运行还是待回收状态。沙箱绑定和回收状态的权威数据已经由 sandbox-manager 写入 Redis，因此第一版以只读监控方式复用这份运行时元数据。

## 变更内容

- `apps/admin-api`：新增 `/admin/sandboxes` 只读接口，从 `cocola:sb:meta:*` 与 lease key 聚合沙箱状态，按需合并 Kubernetes pod 信息，并用 Auth 用户表补齐 username。
- `apps/web`：新增 `/admin/sandboxes` 管理页面和 BFF 代理，展示 sandbox、conversation、user、runtime、created/paused、node/pod 等列，并把入口加入 Admin 总览与侧边栏。
- 测试：补充 admin-api service/http 覆盖状态映射、用户补齐、pod 合并和未配置 501。
- 关键取舍：v1 只展示 Redis 绑定记录对应的沙箱，不展示孤儿 pod，也不提供回收/强杀等操作。
- 后续修正：sandbox-manager reaper 在实际 sandbox 已不存在时会自动解绑 Redis meta；admin-api 监控列表在确认 K8s 无对应 pod 且 lease 已过期时，会兜底清理 stale metadata，避免页面长期残留非 running 记录。
- 后续修正：默认 lease 过期时间从 60 秒调整为 10 分钟，降低长任务或用户短暂停顿时被过早判定为空闲的概率。
