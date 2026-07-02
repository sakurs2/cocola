# feat: composer model picker

- 变更时间：2026-07-03 00:59 (+08:00)

## 变更理由

用户希望参考截图中的输入框布局，将模型切换入口从页面左上角迁移到对话输入框内部，并在输入框中展示 `@` mention 与 `/` commands 的提示文案，让输入区成为更完整的对话控制中心。

## 变更内容

- apps/web/app/page.tsx：移除顶部栏中的静态模型 pill，顶部栏只保留 sandbox 状态。
- apps/web/components/assistant-ui/thread.tsx：重排 composer 为上方输入、下方工具栏结构；在工具栏左侧放置附件按钮和静态模型选择入口；更新输入框 placeholder 为 `Send a message... (@ to mention, / for commands)`。
- 关键取舍：本次只迁移 UI 入口和提示文案，不接入真实多模型切换逻辑。
