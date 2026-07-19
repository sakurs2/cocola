# fix: 使用 GET 探测 Code 编辑器就绪状态

- 变更时间：2026-07-19 19:36 (+08:00)

## 变更理由

Code 面板原先使用 `HEAD` 探测 resident code-server，但 OpenSandbox server proxy 会在请求到达 code-server 前拒绝 `HEAD`，导致 sandbox 实际健康时仍被界面误判为不可用。

## 变更内容

- `apps/web/lib/code-editor-readiness.mjs`：新增基于 `GET` 的编辑器状态探测，取得响应头后主动取消 body，避免重复下载编辑器页面。
- `apps/web/components/assistant-ui/workspace-panel.tsx`：Code 面板统一调用新的探测函数并沿用已有准备中重试、历史 sandbox 回收提示逻辑。
- `apps/web/lib/code-editor-readiness.test.mjs`：覆盖请求方法、缓存策略、AbortSignal 透传、响应体取消和状态码返回。
