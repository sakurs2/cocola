# feat: model icon and answering shimmer

- 变更时间：2026-07-03 11:42 (+08:00)

## 变更理由

对话界面的模型 icon 需要展示真实品牌标志，而不是临时文字占位；同时 agent 回答中的运行态提示放在消息头旁边较抢眼，用户希望移动到回答下方，并使用 Shimmer / Shine 光带扫过动效表达“仍在回答中”。

## 变更内容

- `apps/web/public/brands/deepseek.svg`：新增来自 Simple Icons 的 DeepSeek SVG 标志，替代临时 PNG/文字占位。
- `apps/web/components/assistant-ui/thread.tsx`：模型图片 icon 使用本地 SVG；图标容器背景改为 `bg-card`，与对话框背景一致；`Answering` 状态从消息头移动到回答内容下方。
- `apps/web/app/globals.css`：新增 `aui-answering-shimmer` 光带扫过文字动效，并在 `prefers-reduced-motion` 下禁用动画。
- 关键取舍：保留 `next/image` 并对 SVG 设置 `unoptimized`，既避免 Next lint 的 `<img>` 警告，又不对本地 SVG 做不必要优化。
