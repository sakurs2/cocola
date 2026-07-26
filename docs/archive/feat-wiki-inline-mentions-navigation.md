# feat: 优化 Wiki 引用与目录导航交互

- 变更时间：2026-07-26 11:59 (+08:00)

## 变更理由

用户通过 `@` 选择 Wiki 文件后，引用卡片统一出现在输入框下方，无法看出引用位于问题中的哪个位置；Wiki 左栏采用展开树，和期望的逐级进入文件夹交互不一致，且固定宽度在长文件名场景下不便使用。

## 变更内容

- `apps/web/components/assistant-ui/thread.tsx`：保留 assistant-ui 原生输入与附件生命周期，将 Wiki 引用显示为原文位置上的内联 token；用户编辑或删除 token 文本时同步清理对应引用，不再在输入框下方重复显示 Wiki 附件卡片。
- `apps/web/lib/wiki-composer-reference.ts`：增加可读 `@文件名` 标记、文本分段匹配和重复引用实例 ID 支持，后端继续接收既有结构化 Wiki 引用。
- `apps/web/lib/plan-mode.mjs`：默认输入提示补充 `use @ to select files from Wiki`。
- `apps/web/components/wiki/wiki-workspace.tsx`：文件夹点击改为进入目录，增加返回和面包屑导航；Wiki 侧栏增加 240–520px 的鼠标拖拽与键盘调宽能力。
- `apps/web/lib/wiki-composer-reference.test.mjs`、`apps/web/lib/wiki-workspace-ui.test.mjs`、`apps/web/lib/plan-mode.test.mjs`：覆盖内联引用分段、重复引用、引用删除、目录导航、侧栏调宽及默认提示文案。

## 关键取舍

- 不引入新的富文本输入框。内联 token 使用与原 textarea 等宽的视觉覆盖层，保留原有输入法、快捷键、粘贴附件、自动高度和发送行为。
- Wiki 引用仍通过结构化附件进入现有 `wiki_refs` 链路，展示文案不包含内部 UUID，也不改变 Gateway 或 Agent Runtime 协议。
