# feat: web 聊天 UI 精装返工(补齐 shadcn 设计令牌 + 1:1 对齐官方观感)

日期：2026-07-01 · 关联 ADR：0007(gateway BFF + SSE 契约)/0009(Route-A 沙箱内 Claude Code)/0010(tool-use 透传)· 关联 Plan：docs/plan/web-product-ui-assistant-ui.md

## 背景
上一版接入 assistant-ui 后,实际观感与官方 demo 差距明显:页面像"裸 HTML"——
中性灰、无圆角、无欢迎区、输入框是方框而非药丸。根因有二:

1. **缺设计令牌层**:组件大量引用 shadcn 语义类(`bg-background`、
   `text-muted-foreground`、`border-border` 等),但 `globals.css` 未定义对应
   CSS 变量、`tailwind.config.ts` 未把 Tailwind 颜色名映射到这些变量,导致这些
   类全部解析为空,样式落空。
2. **手搓了精简版而非复用官方设计**:Thread/Button/Markdown 用 `neutral-*`
   硬编码颜色自行拼装,偏离"尽量复用开源"的约束,也丢了官方的欢迎区 + 建议卡 +
   药丸输入条 + 圆形按钮等观感要素。

本次返工补齐令牌层,并用 **assistant-ui 0.14.24 已导出的 primitive API** + 经典
Tailwind 工具类,在 **Tailwind v3** 上还原官方 shadcn 观感(官方 registry 的
canary 代码用 `aui-*` 类,仅适配 Tailwind v4 且依赖 npm 包未发布的配套 CSS,直接
照搬会在 v3 上失效,故采用令牌 + 经典类的等价实现)。

## 复用面与边界
- **不动**:后端、网关 SSE 代理(`app/api/chat/route.ts`)、SSE 契约、
  `app/runtime-provider.tsx` 适配器逻辑(send/cancel 能力不变)。
- **复用**:assistant-ui 0.14.24 已导出的 `ThreadPrimitive`(含
  `Empty`/`Suggestion`/`ScrollToBottom`/`If`)、`ComposerPrimitive`、
  `MessagePrimitive`、`ActionBarPrimitive`;shadcn 标准 slate 调色板与令牌结构。
- **自写**:仅令牌层映射 + 轻量 `TooltipIconButton`(原生 title,不引 Radix
  Tooltip,保持依赖轻量)。

## 改动(apps/web)
### 新增
- `components/assistant-ui/tooltip-icon-button.tsx` —— 包 `Button` 的图标按钮,
  原生 `title`/`aria-label` 提示 + `sr-only` 文案,默认 `ghost`/`icon`。

### 修改
- `app/globals.css` —— 补齐 shadcn slate 调色板:`:root` 与 `.dark` 下的
  `--background`/`--foreground`/`--primary`/`--secondary`/`--muted`/`--accent`/
  `--destructive`/`--border`/`--input`/`--ring`/`--radius` 等全套 HSL 通道变量;
  base 层统一 `border-color` 与 body 前景/背景色。
- `tailwind.config.ts` —— `darkMode: ["class"]`;`theme.extend.colors` 把
  border/input/ring/background/foreground 及 primary/secondary/destructive/
  muted/accent/popover/card(各含 DEFAULT + foreground)映射到上述变量;
  `borderRadius` 接 `--radius`。
- `components/ui/button.tsx` —— 改用完整 shadcn cva 变体
  (default/destructive/outline/secondary/ghost/link × default/sm/lg/icon),
  全部走令牌色与 `focus-visible:ring-ring`。
- `components/assistant-ui/markdown-text.tsx` —— prose 样式改用令牌
  (`text-foreground`、`[&_a]:text-primary`、`[&_code]:bg-muted`、
  `[&_pre]:bg-foreground [&_pre]:text-background`、`[&_blockquote]:border-border`
  等),不再硬编码 neutral。
- `components/assistant-ui/thread.tsx` —— 1:1 还原官方观感:居中欢迎标题
  ("How can I help you today?")+ 副标题 + 四张建议卡(经
  `ThreadPrimitive.Suggestion` 点击即发送);药丸形(`rounded-3xl`)输入条 +
  圆形 Send/Stop 按钮(随 running 切换);消息气泡、思考块、工具折叠卡、Copy
  ActionBar、ScrollToBottom 圆按钮全部改用令牌色与 `TooltipIconButton`。
- `app/page.tsx` —— SessionBar 的边框/背景/输入框由 `neutral-*` 迁移到令牌
  (`border-border`、`bg-background`、`border-input`、`focus:border-ring` 等),
  与正文观感统一。

## 校验
- `corepack pnpm@10 --filter @cocola/web build` —— PASS(路由 `/` 144kB/231kB
  First Load、`/_not-found`、`/api/chat`、`/raw`)。
- `corepack pnpm@10 --filter @cocola/web lint` —— PASS(No ESLint warnings or errors)。
- `prettier --write` —— 全绿(规范后复跑 lint+build 仍绿)。

## 非目标
- 不改后端、网关代理、SSE 契约、适配器能力面(仍仅 send/cancel,不渲染附件/分支控件)。
- 不引入 Radix Tooltip / 官方 canary `aui-*` 样式(Tailwind v4 专属)。
- 沙箱内禁起监听端口,故未引入 `next dev` 预览;校验以 build/lint/prettier 为准。
