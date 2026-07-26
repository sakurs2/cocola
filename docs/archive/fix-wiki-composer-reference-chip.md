# fix: Render Wiki references as composer chips

- 变更时间：2026-07-26 11:23 (+08:00)

## 变更理由

用户通过 `@` 选择 Wiki 文件后，assistant-ui 的 Directive 行为会把内部协议文本直接写入输入框，例如 `:wiki-file[用户调查体验.md]{name=<node-id>}`。该文本暴露实现细节、难以阅读，也不符合文件引用的交互预期。

## 变更内容

- `apps/web/components/assistant-ui/thread.tsx`：Wiki 菜单选择改为 Action 行为，移除 `@` 查询文本并添加专用 Wiki 文件 Chip；Chip 支持去重、删除、仅引用文件发送和发送后自动清空。
- `apps/web/lib/wiki-composer-reference.ts`：定义 Wiki composer 附件协议、解析与去重逻辑。
- `apps/web/app/runtime-provider.tsx`：发送时将结构化 Wiki 附件转换为既有 `wiki_refs` 请求，并保留旧 Directive 文本的兼容解析；仅引用文件时补充最小非空提示，满足 Gateway 的请求约束。
- `apps/web/lib/wiki-composer-reference.test.mjs`：覆盖结构化附件、普通附件隔离、异常数据和引用去重。

## 关键取舍

- 复用 assistant-ui 原生附件生命周期，不引入隐藏字符、自定义富文本输入框或额外全局状态。
- 前端展示结构和后端传输协议分离，Agent、Gateway 与 Wiki 快照链路无需调整。
