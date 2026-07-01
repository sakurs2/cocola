# fix: ClaudeAgentSDKProvider 逐 token 流式(开 include_partial + 映射 StreamEvent 增量)

日期：2026-07-01

## 背景
用户反馈聊天「不是流式输出」——答案在模型跑完后一次性弹出,而非逐字打出。
排查三段链路:
- gateway(`httpapi/api.go`):每个事件 `writeSSE` 后立即 `flusher.Flush()`,并设
  `x-accel-buffering: no`,**逐帧下发正常**。
- 前端(`runtime-provider.tsx`):`reader.read()` 循环 + `parseFrames`,`text` 事件经
  `appendTo` 增量拼接进在途 assistant message,**消费正常**。
- **根因在 provider**:Claude Agent SDK 默认 `include_partial_messages=False`,
  一轮只在结束时 yield 一个完整 `AssistantMessage`(整块 `TextBlock`)。
  `_message_to_events` 也没有 `StreamEvent` 分支。于是整段答案只对应**一个** `text`
  事件——后两段就算逐帧 flush,也只有一帧可流。
  (Route A 的 shim provider 走 stream-json 已逐 delta 输出,不受影响。)

## 改动(apps/agent-runtime/cocola_agent_runtime/claude_sdk_provider.py)
- 构造器记录 `self._include_partial = query_fn is None`:仅真实 SDK 路径开 token 流式;
  注入 fake 的单测保持「一条 AssistantMessage → 一个 text 事件」的旧语义。
- `_build_options`:真实路径传 `include_partial_messages=True`,让 SDK 逐 token 发
  `StreamEvent`。
- 新增 `_stream_event_to_events`:把原始 Anthropic 流事件里 `content_block_delta` 的
  `text_delta` / `thinking_delta` 映射为增量 `text` / `thinking` 事件;空 delta 与
  message_start/stop、ping、block start/stop、usage 等控制帧一律丢弃。
- `_message_to_events(message, include_partial)`:加 `StreamEvent` 分支;开流式时
  **跳过**结尾 `AssistantMessage` 里重复的 `TextBlock`/`ThinkingBlock`(避免整段答案
  被渲染两次),`ToolUseBlock`/`error` 仍照常透出。

## 校验
- 新增 5 个单测(StreamEvent→text/thinking、控制帧丢弃、流式去重、非流式保留全块)。
- `pytest -q` → 61 passed, 2 skipped(较前 +5);`ruff check` 全绿。

## 非目标
- Route A(shim)路径本就逐 delta 输出,未改动。
- 端到端逐字观感需本地 `make up-all` 实测(沙箱不能起监听进程)。
