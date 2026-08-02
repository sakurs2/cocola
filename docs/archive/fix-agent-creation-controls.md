# fix: Refine Agent creation and editing controls

- 变更时间：2026-08-03 00:10 (+08:00)

## 变更理由

Agent 页面仍显示已经决定移除的 Test Agent 操作，主按钮使用渐变色；新建 Agent 时默认图标看起来无法保存，而且创建弹窗无法直接选择最终图标和颜色。这些行为让创建状态和已保存状态难以区分。

## 变更内容

- `apps/web/app/agents/[id]/page.tsx`：移除 Test Agent，未修改时明确显示 Saved，修改后才显示 Save。
- `apps/web/app/agents/page.tsx`：创建弹窗支持选择并持久化图标与颜色，同时限制弹窗高度并允许滚动。
- `apps/web/app/globals.css`：Agent 页面主按钮改用纯色视觉。
- `apps/web/lib/agent-capabilities.test.mjs`：覆盖移除 Test Agent、默认图标创建、图标颜色持久化和纯色按钮。
