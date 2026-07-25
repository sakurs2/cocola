# fix: 修复 Wiki 轮次快照、上传缓冲与版本链接

- 变更时间：2026-07-26 00:36 (+08:00)

## 变更理由

Wiki 复审发现三条高影响链路：Runtime 会在复用 Sandbox 时保留上一轮的
`/workspace/wiki`，导致未再次引用的文件仍可见，并可能在文件/目录类型变化时写入失败；
Web 代理会在 Gateway 校验上传大小前将完整请求体读入内存；带 Wiki 引用的乐观用户消息
不会在启动成功后对账到不可变文件版本，导致当前节点更新后历史消息下载到新内容。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/server.py`：每轮在固定 Wiki 根目录上重建快照；
  无引用轮次也清空目录，有引用轮次先完成大小、哈希和路径校验再替换内容，清理失败时
  终止本轮，避免 Agent 读取旧文件。
- `apps/web/lib/wiki-proxy.ts`、`apps/web/lib/wiki-proxy-request.ts`：Next.js Node Route
  Handler 直接把请求 `ReadableStream` 转发给 Gateway，并设置 Node fetch 所需的
  `duplex: "half"`，保留 Gateway 的单文件大小限制作为权威边界。
- `apps/web/app/runtime-provider.tsx`、`apps/web/lib/wiki-message-reconciliation.ts`：
  获取 `runId` 后仅对账本轮持久化的用户消息，将 Wiki 卡片切换到不可变版本下载地址，
  不覆盖正在流式更新的 assistant 消息。
- `apps/agent-runtime/tests/`、`apps/web/lib/wiki-*.test.mjs`：增加空引用清理、清理失败、
  文件/目录类型切换、请求体流式转发和用户消息定点对账回归测试。
- 保持简单实现：不增加 Markdown 自动保存、不增加 Wiki 内容缓存或额外配额系统。

## 验证

- `uv run pytest`：258 passed，2 skipped。
- `node --test apps/web/lib/*.test.mjs`：82 passed。
- `pnpm exec tsc -p apps/web/tsconfig.json --noEmit`
- `pnpm --filter @cocola/web lint`
