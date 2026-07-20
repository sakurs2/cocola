# fix: Folder Composer 只保留一层边框

- 变更时间：2026-07-21 00:56 (+08:00)

## 变更理由

Folder 页面在 ConversationComposer 自带边框之外又包裹了一层带边框、内边距和阴影的卡片，导致输入区域呈现不必要的双层框体。

## 变更内容

- `apps/web/app/folders/[id]/page.tsx`：移除 Composer 外层的边框、背景、内边距和阴影，只保留布局间距以及 Composer 自身的交互边界。
