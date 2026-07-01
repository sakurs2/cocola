# fix: 用户消息气泡短词被拦腰断行(hello -> he/llo)

日期：2026-07-01 · 关联：feat-web-ui-openwebui-look.md(Open WebUI 观感改造)

## 现象
窄视口下,用户发送的短词(如 "hello")在气泡内被从单词中间强行折成两行
("he" / "llo")。

## 根因
用户气泡用了 `break-words`(`overflow-wrap: break-word`)。该值不参与元素最小
内容宽度计算,当气泡被 grid 列压窄到比单词还窄时,会在字母间强行断行。而气泡的
`max-w-[80%]` 本应让它自然收窄而非劈开短词。

## 改动(apps/web)
- `components/assistant-ui/thread.tsx` —— 用户气泡换行策略由 `break-words` 改为
  `[overflow-wrap:anywhere]` + `whitespace-pre-wrap`。`anywhere` 会把字内断点计入
  最小内容宽度,使气泡优先靠 `max-w-[80%]` 收窄,仅在超长无空格串确实放不下时
  才断行;`whitespace-pre-wrap` 保留用户输入里的换行与连续空格。

## 校验
- lint PASS(next lint,无告警)
- build PASS(146 kB / 233 kB,与既有一致)

## 非目标
未改后端、SSE 契约、runtime 适配器;助手消息气泡与工具卡片样式不变。
