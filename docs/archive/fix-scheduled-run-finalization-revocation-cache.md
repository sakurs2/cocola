# fix: 修复定时任务终态、对话落库恢复与吊销缓存增长

- 变更时间：2026-07-27 13:57 (+08:00)

## 变更理由

- Admin API 只要成功读完 Gateway 的 SSE 响应就会把定时任务记为成功，没有校验 `done` 事件中的真实终态，导致执行错误、取消或中断也被误报为成功。
- Gateway 对话执行结束时仅有限次写入终态；数据库短时不可用且进程继续运行时，本地执行状态会被清理，而数据库中的 Run 可能永久停留在 `running`。
- LLM Gateway 的 JWT 吊销 TTL 缓存没有容量上限，高基数 Token ID 会让已过期条目持续占用内存。

## 变更内容

- `apps/admin-api/internal/service/scheduler.go`：要求 Gateway SSE 必须包含 `done/status=success`；缺失终态或其他状态均作为任务失败返回。
- `apps/admin-api/internal/service/scheduled_tasks_test.go`：覆盖成功、错误、取消、中断、等待输入、缺失终态和缺失状态。
- `apps/gateway/internal/httpapi/simple_chat.go`：在持久化终态确认前保留本地 Run；数据库失败后使用可被关闭信号中断、最长 30 秒退避的恢复循环，优先重试原始终态，必要时再写入 `interrupted/FINALIZATION_FAILED` 安全终态。
- `apps/gateway/internal/httpapi/simple_chat_test.go`：覆盖终态写入的有限重试、安全兜底，以及数据库恢复后保留原始成功终态并释放本地 Run。
- `apps/llm-gateway/cocola_llm_gateway/auth/revocation.py`：将 TTL 缓存改为最多 10,000 条的 LRU 缓存，读取时清理过期命中，TTL 为 0 时不写入条目。
- `apps/llm-gateway/tests/test_revocation.py`：覆盖容量限制、LRU 淘汰和被淘汰 Token 的后端重新查询。
- 关键取舍：不修改 P0 权限链路；不引入新的后台任务或持久化队列，保持修复集中在现有执行与缓存路径。
