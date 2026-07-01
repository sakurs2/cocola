# fix: 模型响应中显示加载指示器(assistant 气泡打字动画)

日期：2026-07-01

## 背景
用户反馈「模型回答的时候对话框应该有 UI(例如转圈)表示正在加载中」。此前 assistant
气泡在首个 token 落地前是空的,用户无从判断模型是否在工作。

## 改动
### apps/web(components/assistant-ui/thread.tsx)
- runtime-provider 在一轮开始时先 push 一条**空**的 assistant message,随后才把文本
  流式写入。因此「`hasContent=false` 且是最后一条」恰好等价于「已发起、尚未出字」的
  在途状态。
- 在 `AssistantMessage` 内用 `<MessagePrimitive.If hasContent={false}>` 嵌套
  `<MessagePrimitive.If last>` 命中该状态,渲染新增的 `TypingIndicator`。
- `TypingIndicator`:三点跳动(纯 CSS,Tailwind `animate-bounce` + 交错
  `animation-delay`),`role="status"` + `aria-label` 供读屏播报。首个 token 到达后
  `parts.length>0`,条件自动关闭,动画消失。

## 校验
- `tsc --noEmit` → 0 error。
- `next lint`(thread.tsx)→ No ESLint warnings or errors。

## 非目标
- 未改动运行时/取消逻辑;停止按钮(`ActionBarPrimitive` hideWhenRunning)行为不变。
