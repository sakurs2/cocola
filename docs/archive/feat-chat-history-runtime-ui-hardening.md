# feat: chat history runtime and UI hardening

- 变更时间：2026-07-03 13:49 (+08:00)
- 关联提交：待提交

## 变更理由

近期围绕 cocola agent 对话体验和运行可靠性做了一组连续修复：

- 侧边栏 chat history 需要展示后台会话运行态、完成态，并支持重命名 / 删除。
- 删除会话时需要释放对应 sandbox，避免资源残留。
- 历史会话继续提问时，旧 sandbox 可能已被底层 OpenSandbox/Docker 清理，继续复用会触发 sandbox not found。
- DeepSeek Anthropic-compatible 上游对 Claude Code 工具调用历史校验更严格，编译 / 运行代码时可能因 tool_use/tool_result 序列被拒绝。
- 前端需要更明确的 agent 回答进行中状态、完成提示和输入框层级，避免滚动历史时视觉重叠。

## 变更内容

- apps/web/app/runtime-provider.tsx：暴露 per-conversation running/completed 状态，支持会话重命名、删除、后台流式回答状态维护。
- apps/web/components/assistant-ui/app-sidebar.tsx：新增运行 spinner、完成对号、行内重命名和自定义删除弹窗。
- apps/web/components/assistant-ui/thread.tsx、apps/web/app/globals.css：新增回答中流光边框、Answering 动效、输入框不透明层级修复。
- apps/web/app/api/conversations/[id]/route.ts：新增会话 PATCH/DELETE same-origin proxy。
- apps/gateway/internal/convo、apps/gateway/internal/httpapi：新增会话重命名 / 删除 API，并在删除时 best-effort 释放 runtime session。
- packages/proto/cocola/agent/v1/agent.proto 及生成代码：新增 ReleaseSession RPC。
- apps/agent-runtime/cocola_agent_runtime/server.py：实现 ReleaseSession，释放 sandbox binder 并清理 session_map。
- apps/agent-runtime/cocola_agent_runtime/session_map.py、shim_provider.py：记录并校验 resume 所属 sandbox，sandbox 换代后丢弃旧 Claude resume。
- apps/sandbox-manager/internal/orchestrator/binder.go：复用 active sandbox 前进行 provider health 校验，发现底层 sandbox 丢失时清理 stale binding 并重建。
- apps/llm-gateway/cocola_llm_gateway/upstream/anthropic.py：在 Anthropic-compatible 出口规整 tool_use/tool_result 序列，补充结构化诊断日志。
- 相关 Go / Python / Web 测试：覆盖会话管理、ReleaseSession、stale sandbox、session map、DeepSeek tool-use transcript 兼容与前端 lint。

## 验证

- `./node_modules/.bin/next lint`
- `GOWORK=off GOCACHE=/private/tmp/cocola-go-build /Users/bytedance/.gvm/gos/go1.24.3/bin/go test ./internal/orchestrator`
- `UV_CACHE_DIR=/private/tmp/cocola-uv-cache /opt/homebrew/bin/uv run --project apps/agent-runtime pytest apps/agent-runtime/tests/test_session_map.py apps/agent-runtime/tests/test_shim_provider.py`
- `UV_CACHE_DIR=/private/tmp/cocola-uv-cache /opt/homebrew/bin/uv run --project apps/llm-gateway pytest apps/llm-gateway/tests/test_tool_use_passthrough.py`
- `UV_CACHE_DIR=/private/tmp/cocola-uv-cache /opt/homebrew/bin/uv run --project apps/llm-gateway ruff check apps/llm-gateway/cocola_llm_gateway/upstream/anthropic.py apps/llm-gateway/tests/test_tool_use_passthrough.py`
