# fix: 收紧 Wiki 请求、目录并发与未保存导航边界

- 变更时间：2026-07-25 23:50 (+08:00)

## 变更理由

Wiki 首版复审发现三条高影响路径：单轮对话可提交无上限的 Wiki 引用并放大数据库和 Runtime 内存消耗；文件创建与目录软删除没有参与同一把结构锁，可能留下指向已删除目录的活动节点；Markdown 的 `beforeunload` 保护无法覆盖 Next.js 客户端导航，用户通过侧边栏离开时会静默丢失未保存内容。

本次继续遵循简单实现原则，不增加用户存储配额、自动保存、数据库表、全局浏览器 history 拦截或新的运行时配置。

## 变更内容

- `apps/gateway/internal/httpapi/simple_chat.go`：单轮最多接受 20 个 Wiki 引用、总大小最多 100 MiB，超限在创建对话运行前返回明确错误。
- `apps/gateway/internal/wiki/postgres.go`：目录创建、文件创建、移动和递归删除共用 owner 级事务 advisory lock；批量解析引用只加载一次目录树，再按不可变版本执行点查询。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：在对象存储读取前重复校验引用数量和声明总大小，避免绕过 Gateway 时产生无界资源消耗。
- `apps/web/components/assistant-ui/workspace-unsaved-changes.tsx` 及 Workspace 组件：共享一个轻量 dirty 状态，侧边栏和命令面板离开 Wiki 前统一确认；继续保留刷新和关闭页面的原生提示。
- Go、Python 回归测试：覆盖引用数量和总大小边界以及 Runtime 在对象读取前失败的行为。

## 验证

- `go test ./internal/wiki ./internal/httpapi`
- `uv run pytest tests/test_wiki_provisioning.py`
- `pnpm exec tsc --noEmit --pretty false`
- `uv run ruff format`、`uv run ruff check --fix`
- `gofmt -w -s`
