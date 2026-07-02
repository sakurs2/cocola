# feat: agent response rendering and stale sandbox recovery

- 变更时间：2026-07-03 00:47 (+08:00)

## 变更理由

对话 UI 需要更适合 agent 场景的回答渲染，包括更清晰的 Markdown、可复制且带轻量语法高亮的代码块、可折叠 reasoning 和工具调用结果。

同时，`make up` 后页面发起会话可能遇到 `sandbox acquire failed`：Redis 中仍保留 session 到 paused sandbox 的绑定，但 OpenSandbox/Docker 侧 sandbox 已不存在，恢复时返回 404，旧逻辑直接把错误返回给用户。

## 变更内容

- apps/web/components/assistant-ui/markdown-text.tsx：增强 Markdown 样式；通过 assistant-ui 的 `CodeHeader` / `SyntaxHighlighter` 插槽实现代码块语言标签、复制按钮和轻量语法高亮，不新增依赖。
- apps/web/components/assistant-ui/thread.tsx：将 reasoning 渲染为默认折叠面板；将 tool call 渲染为带状态、参数/结果分区和错误态的 agent 工具卡片。
- apps/web/app/globals.css：增加轻量入场动画和 reduced-motion 保护。
- apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go：OpenSandbox 404 错误包装为 `fs.ErrNotExist`，供上层识别 stale sandbox。
- apps/sandbox-manager/internal/orchestrator/binder.go：恢复 paused sandbox 遇到 not-exist 时清理旧绑定并重新创建 sandbox。
- apps/sandbox-manager/internal/orchestrator/binder_test.go：补充 stale paused sandbox 自动重建的回归测试。
