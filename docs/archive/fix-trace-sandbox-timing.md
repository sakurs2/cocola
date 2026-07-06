# fix: trace 展示沙箱准备耗时

- 变更时间：2026-07-06 20:06 (+0800)
- 关联提交：待提交

## 变更理由

审计日志进入 trace 分析后只能看到 gateway、agent stream、持久化和审计阶段，看不到沙箱创建/复用的具体等待耗时。对话启动慢时，无法判断耗时是在 OpenSandbox acquire、checkpoint restore、附件写入沙箱，还是后续 agent 执行。

## 变更内容

- apps/agent-runtime/cocola_agent_runtime/server.py：新增内部 `trace` 事件 helper，并在 `sandbox.create` / `sandbox.reuse`、`sandbox.checkpoint_restore`、`sandbox.attachments_provision` 成功/失败路径记录耗时。
- apps/gateway/internal/httpapi/api.go：消费 agent-runtime 内部 `trace` 事件并写入 `trace_events`，不转发到用户 SSE；同时用普通 `sandbox` 事件补充 `sandbox.ready_wait` 粗粒度兜底。
- apps/web/app/admin/traces/[traceId]/page.tsx：将 `sandbox.*` 阶段归入 `Agent Runtime` 模块下展示，保留行级 `sandbox` 分类，体现 sandbox 是 agent-runtime 下级阶段。
- apps/gateway/internal/httpapi/api_test.go、apps/agent-runtime/tests/test_sandbox_binding.py、apps/agent-runtime/tests/test_server.py：补充/更新测试，覆盖内部 trace 不泄漏到 SSE 以及新增事件序列。
