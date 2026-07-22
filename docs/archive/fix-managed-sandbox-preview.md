# fix: 修复 Project 对话布局与 Preview 生命周期

- 变更时间：2026-07-22 12:44 (+08:00)

## 变更理由

Project Task 页在固定面包屑下仍让对话工作区占满父容器高度，导致底部 Composer 向下溢出并被裁切。Agent 使用普通 Bash 后台任务启动开发服务器时，进程又会在 Agent 回合结束后随运行时清理退出，因此 Preview Proxy 虽然能解析 Sandbox 地址，却无法连接已消失的 3000 端口并直接展示 502 JSON。

## 变更内容

- `apps/web/app/projects/[id]/tasks/[conversationId]/page.tsx`：将对话工作区约束到面包屑以下的剩余高度。
- `apps/web/components/assistant-ui/workspace-panel.tsx`：在挂载 iframe 前探测 Preview 服务，增加连接中、不可用、重试状态，避免直接渲染代理错误正文。
- `deploy/sandbox-runtime/cocola_sandbox.py`、`runtime-manifest.json`：新增有界启动等待、进程身份校验、凭证环境清理和有限日志轮转的托管 Preview 进程。
- `deploy/sandbox-runtime/skills/cocola-sandbox-preview/SKILL.md`、`skills/manifest.json`：内置 Preview Skill，要求服务监听 `0.0.0.0` 并通过托管命令确认 Ready。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：仅在 Runtime 镜像声明 Preview Skill 时注入对应系统指引，兼容滚动升级。
- `apps/agent-runtime/tests/`、`deploy/sandbox-runtime/README.md`：覆盖托管进程、日志边界、PID 复用、路径约束和 Prompt 注入，并补充使用文档。
