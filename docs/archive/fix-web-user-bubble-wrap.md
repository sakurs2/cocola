# fix: 用户消息气泡短词被拦腰断行(hello -> he/llo)

日期：2026-07-01 · 关联：feat-web-ui-openwebui-look.md(Open WebUI 观感改造)

## 现象
用户发送的短词(如 "hello")在气泡内被从单词中间强行折成两行("he" / "llo")。

## 根因(修订)
> 说明：本文件曾记录过一次"改 overflow-wrap 即可"的修复,但那次未能解决问题——
> 下面是复盘后确认的真正根因。

用户气泡自身用了**百分比** `max-w-[80%]`,而它所在的 grid 列宽是 `auto`——列宽
由内容决定,气泡的百分比 `max-width` 又相对该列宽计算,形成循环依赖。

按 CSS 规范,这种"百分比 max-width 依赖于收缩包裹(shrink-to-fit)列宽"的情况下,
浏览器解析时先把百分比 `max-width` 当作 `none` 算出列的 `max-content` 宽度
(= "hello" 的完整宽度),再对气泡套用 80%。于是气泡的实际 `max-width`
= 0.8 × "hello" 宽度 < "hello" 宽度,单词放不下 → 被拦腰断开。

叠加 `[overflow-wrap:anywhere]` 只会让它"更愿意断",反而加重现象。这偏离了官方
assistant-ui 用户气泡的写法。

## 改动(apps/web)
- `components/assistant-ui/thread.tsx` —— 用户气泡的 `max-width` 由百分比
  `max-w-[80%]` 改为**固定长度** `max-w-[calc(var(--thread-max-width)*0.8)]`
  (对齐官方 assistant-ui 写法)。该长度不依赖列宽,气泡在放得下短词的前提下自然
  收窄,短词不再被压断。换行策略回归 `break-words` + `whitespace-pre-wrap`:
  `break-words` 仅在真正的超长无空格串(如长 URL)时断行,`whitespace-pre-wrap`
  保留用户输入的换行与连续空格。

## 校验
- prettier PASS(code style 一致)
- lint PASS(next lint,无告警)
- build PASS(146 kB / 233 kB,与既有一致)

## 非目标
未改后端、SSE 契约、runtime 适配器;助手消息气泡与工具卡片样式不变。
