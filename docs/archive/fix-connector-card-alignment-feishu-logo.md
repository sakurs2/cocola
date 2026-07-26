# fix: 对齐 Connector 卡片并使用飞书品牌图标

- 变更时间：2026-07-26 18:46 (+08:00)

## 变更理由

GitHub 卡片缺少连接状态行，导致其主操作按钮与飞书卡片无法水平对齐；飞书卡片此前使用的临时交叉图形也不是飞书品牌 Logo。

## 变更内容

- `apps/web/app/connectors/page.tsx`：为 GitHub 卡片增加与飞书一致的动态连接状态行。
- `apps/web/components/connectors/feishu-connector-card.tsx`：用独立的飞书品牌图标资源替换临时图形，并保留现有卡片尺寸。
- `apps/web/public/feishu-logo.svg`：新增飞书三色品牌 Logo 静态资源。
- `apps/web/lib/feishu-connector-ui.test.mjs`：增加状态行和品牌图标的静态回归检查。
