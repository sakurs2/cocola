# fix: 修复对话假成功、会话 owner 与 Warm Pool 健康判断

- 变更时间：2026-07-13 13:26 (+08:00)

## 变更理由

核心对话链路审查发现四个确定问题：Claude SDK 的错误 Result 被当作成功 Run；checkpoint
元数据可能生成空 owner 并阻塞后续 resume；Binder 与 Warm Pool 只检查 Health 调用错误而忽略
实际健康状态；单飞 409 被 Web 代理包装为成功 SSE，导致被拒绝的问题与已有 Run 回答错配。

本次按最小修复原则处理上述问题，明确不改变 checkpoint 上传失败后 idle reaper 的既有回收
语义。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/shim_provider.py`：将
  `ResultMessage.is_error=true` 映射为明确的 Agent error，避免失败 Run 保存为 success；SessionMap
  写入失败也产生错误终态，不再让下一轮静默失忆。
- `deploy/sandbox-runtime/shim/agent_shim.py`：将 Claude SDK 的 dangling resume `ProcessError`
  在 shim 边界收敛成 `RESUME_NOT_FOUND` 协议码；Runtime 不再扫描任意 stderr，也不再把所有 timeout
  猜测成浏览器操作问题。
- `apps/sandbox-manager/internal/provider/checkpoint/checkpoint.go`：checkpoint 成功和失败元数据都
  显式携带 `user_id`，仅允许同 owner 更新并校验实际写入行数。
- `db/migrations/00033_fix_checkpoint_session_ownership.sql`：回填可确认 owner 的历史空 owner
  SessionMap，删除无法安全确认的孤儿索引。
- `apps/sandbox-manager/internal/orchestrator`、`internal/provider`：区分 Running、过渡态和终态；
  Warm Pool 只 claim Running，保留 Pending，清理终态；已有终态 binding 安全重建。
- `apps/web/app/api/chat/route.ts`、`app/runtime-provider.tsx`：流建立前保留 Gateway 的真实 HTTP
  状态；409 回滚乐观消息、刷新真实历史并跟随已有 Run。
- `apps/gateway/internal/httpapi`：对话删除只在检查 active Run 和删除数据库记录时持有 mutation
  lock；最长 10 秒的 Runtime session release 移到锁外，不再阻塞其他会话启动。
- 测试：补充 Agent 错误 Result、Warm Pool Pending/Failed、绑定终态重建、OpenSandbox 过渡态和
  checkpoint owner 校验；未引入新的测试框架或生产依赖。

关键取舍：沿用单 Gateway、数据库单飞和 snapshot recovery，不增加消息队列、分布式协调或新的
运行状态。
