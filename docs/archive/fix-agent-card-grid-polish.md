# fix: 优化 Agent 列表卡片布局与悬浮质感

- 变更时间：2026-07-28 01:18 (+08:00)

## 变更理由

Agents 列表原本在桌面端固定为两列，单张卡片较宽且内容集中在左上方；卡片悬浮时还会发生位移，整体显得空旷且缺少稳定的表面层级。用户确认采用三列卡片、静止悬浮和精细边框的设计，并要求模型信息沿用对话框下方的品牌图标加文字组合。

## 变更内容

- `apps/web/app/agents/page.tsx`：桌面端改为三列、平板两列、手机一列；新增浅灰列表托盘，卡片悬浮只改变背景、边框、阴影和操作按钮，不再位移；底部使用模型品牌图标和别名。
- `apps/web/components/ui/model-icon.tsx`：从对话线程中提取共享模型图标组件，供对话框、只读会话和 Agent 卡片复用。
- `apps/web/components/assistant-ui/thread.tsx`、`apps/web/components/conversation-readonly.tsx`：切换到共享模型图标组件，保持既有显示行为。
- `apps/web/lib/model-icons.ts`、`apps/web/app/runtime-provider.tsx`、`apps/web/lib/agents.ts`：统一模型图标类型与输入归一化，Agent 模型目录保留图标配置。
- 正式卡片只使用已有的名称、描述、头像和模型字段，没有引入 Demo 中的示例分类或额外产品字段。
