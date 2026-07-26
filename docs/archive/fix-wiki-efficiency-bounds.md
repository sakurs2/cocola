# fix: 收紧 Wiki 查询与 Office 读取资源边界

- 变更时间：2026-07-26 01:38 (+08:00)

## 变更理由

Wiki 效率专项审查发现五条高影响链路：带 Wiki 引用的消息为对账单条持久化
消息会重新下载整段会话历史；聊天框未使用 `@` 时也会提前加载整棵 Wiki；
单文件读取和批量引用解析会扫描用户全部 Wiki 节点并产生 N+1 查询；XLSX
允许近乎无界的读取范围；DOCX、PPTX 和 XLSX 提取缺少展开量与输出上限。

这些行为会让网络、数据库、浏览器和沙箱资源消耗随会话长度、Wiki 节点数量
或 Office 文档规模无界增长。修复继续保持简单实现，不增加自动保存、Wiki
内容缓存、用户配额或新的持久化表。

## 变更内容

- `apps/gateway/internal/convo/`、`apps/gateway/internal/httpapi/api.go`：增加
  owner-scoped 单消息点查；`message_id` 请求跳过完整历史及计划、问题、Run
  状态聚合。
- `apps/web/app/runtime-provider.tsx`、
  `apps/web/app/api/conversations/[id]/messages/route.ts`：Wiki 乐观消息只回读
  `${runId}-user`，并由 Next.js 代理显式透传 `message_id`。
- `apps/web/components/assistant-ui/thread.tsx`：`@` Wiki 目录在弹窗首次打开时
  才加载，在同一 Thread 生命周期内共享、去重，并在 Wiki 变更后失效。
- `apps/gateway/internal/wiki/postgres.go`：单文件路径改为目标节点祖先链递归
  查询；批量引用通过一条 SQL 保序读取目标节点、当前版本和逻辑路径。
- `deploy/sandbox-runtime/wiki_reader.py`：XLSX 单次最多读取 10,000 个单元格；
  Office 声明展开量最多 64 MiB、条目最多 10,000 个、输出最多 1 MiB，并在
  截断时返回明确提示。
- Go、Python 回归测试：覆盖单消息 owner 隔离、API 定点过滤、XLSX 范围拒绝、
  Office 展开量拒绝和输出截断。

## 验证

- Gateway：`go test ./...`
- Agent Runtime：`uv run pytest`（261 passed，2 skipped）
- Web：`node --test apps/web/lib/*.test.mjs`（82 passed）
- Web：`pnpm exec tsc -p apps/web/tsconfig.json --noEmit --pretty false`
- Web：`pnpm --filter @cocola/web lint`
- PostgreSQL：事务内验证两层目录路径与批量引用顺序，随后回滚
