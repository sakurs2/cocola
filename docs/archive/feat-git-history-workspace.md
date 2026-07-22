# feat: Workspace Git 提交历史与结构化 Diff

- 变更时间：2026-07-22 14:07 (+08:00)

## 变更理由

Project Task 原有 Git Tab 只展示工作区状态和原始文本 Diff，用户无法在 Agent 对话侧边栏中直观查看当前分支的提交历史、提交元数据及某次提交涉及的文件。需要提供类似 GitLens 的只读 Git 体验，同时避免打开侧边栏就唤醒已回收的 Sandbox，也不能引入后台轮询。

## 变更内容

- `packages/proto/cocola/agent/v1/agent.proto` 及生成代码：扩展 Git Inspect 协议，增加有界提交历史、提交详情、提交文件列表和指定提交 Diff。
- `apps/agent-runtime/cocola_agent_runtime/project_git.py`、`server.py`：采集当前分支最近 50 条提交；按需读取提交详情、增删统计和单文件 Diff，并保持路径、文件数和 Diff 大小限制。
- `apps/gateway/internal/agent/`、`internal/project/`、`internal/httpapi/projects.go`：透传提交检查请求、持久化精简历史快照，并继续通过已保存快照支持不唤醒 Sandbox 的离线浏览。
- `apps/web/components/assistant-ui/workspace-panel.tsx`：将 Git Tab 改为 Working Tree、提交图轨、提交详情和文件 Diff 的分层只读界面；支持 unified/split Diff、HEAD/Base/Tag 标识和并发请求防竞态。
- `apps/web/lib/git-history.mjs`、`react-diff-view`：增加 Git 展示纯函数与结构化 Diff 组件，不引入定时轮询。
- Runtime、Gateway 和 Web 测试覆盖历史上限、提交详情、协议映射、时间/引用格式化及提交正文去重。

## 验证

- Agent Runtime：`66 passed`。
- Gateway：`go test ./internal/agent ./internal/project ./internal/httpapi` 通过。
- Web：单元测试、TypeScript、ESLint 和生产构建通过。
- Proto：`buf format --diff --exit-code` 通过；`buf lint` 仍被仓库存量 RPC response 命名规则阻塞，与本次字段扩展无关。
