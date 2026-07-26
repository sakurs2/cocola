# fix: 修复 Wiki Markdown 编辑和行内 @ 触发

- 变更时间：2026-07-26 13:08 (+08:00)

## 变更理由

打开 Markdown 文件时，Tiptap 在同步可编辑状态的过程中会触发一次内容更新。空文件可能因此提前进入已保存状态，而实际内容请求完成后又没有产生新的 React 渲染，最终页面持续显示为只读。内容加载失败时也缺少明确提示，只呈现为不可操作的编辑器。

assistant-ui 的触发器默认要求 `@` 位于输入开头或空白之后，导致用户无法在一句话的任意位置召回 Wiki 文件。

## 变更内容

- `apps/web/components/wiki/wiki-markdown-editor.tsx`：切换 Tiptap 可编辑状态时禁止发出内容更新事件。
- `apps/web/components/wiki/wiki-workspace.tsx`：使用 React state 表达内容是否加载完成，并增加明确的加载失败状态和重试入口。
- `patches/@assistant-ui__react@0.14.24.patch`、`package.json`、`pnpm-lock.yaml`：仅为 `@` 放宽前置空白限制；斜杠命令及其它触发器仍保留原边界规则。
- `apps/web/lib/wiki-markdown-editor.test.mjs`、`apps/web/lib/wiki-workspace-ui.test.mjs`、`apps/web/lib/wiki-mention-trigger.test.mjs`：覆盖编辑状态同步、Markdown 加载状态和任意位置 `@` 触发行为。
- 本次不增加 Markdown 自动保存，继续由用户显式点击保存。
