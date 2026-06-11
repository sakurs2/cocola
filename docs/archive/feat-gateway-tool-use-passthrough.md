# feat(llm-gateway): tool-use passthrough (ADR-0010)

## 背景

M3 把 gateway 做成了纯文本中继:归一化层只承载纯文本,前端 codec 把
`tools` / `tool_choice` 和 content-block(tool_use / tool_result / image)
全部丢弃。结果沙箱内 agent 让模型用工具时永远收不到 `tool_use`——因为工具
*定义*在到达上游前就被剥掉了。这不是 tool_use 难做,而是 M3 简化自己造成的
自缚阻塞点(开源代理只要转发 `tools` 就支持)。

实测证明链路:入站请求带 `tools` → 归一化 `ChatRequest` 无 tools 概念 →
上游 payload 无 `tools`。shim(ADR-0009)与上游代理均正常,缺口在 gateway
的 normalize/encode 层。

## 决策

采用 ADR-0010 的 **Option B:Anthropic 富载荷不透明透传**。归一化层保持
中立但无损,改动全部为附加式,现有纯文本 provider / 测试不受影响。

## 改动(4 个文件 + 1 个测试)

- `types.py`
  - `ChatMessage.content_blocks: list[dict] | None`(原始 Anthropic 块数组,
    与扁平化 `content` 并存)。
  - `ChatParams.tools: list[dict]` / `tool_choice: dict | None`(不透明转发)。
  - `StreamEventType.PASSTHROUGH`(原样中继上游 content-block 帧,
    `extra["frame"]` 携带原始 JSON)。

- `anthropic_codec.py`(入站)
  - `AnthropicRequest` 新增 `tools` / `tool_choice`。
  - `_has_non_text_block()` 判定;`to_chat_request` 保留非文本块到
    `content_blocks`,同时仍扁平化文本供计费。

- `anthropic_codec.py`(出站)
  - 流式:`message_start` 与合成文本块解耦;PASSTHROUGH 帧原样发出;
    legacy `CONTENT_DELTA` 仍走单一 index-0 文本块。两模式互斥,索引不冲突。
  - 非流式:从 PASSTHROUGH 帧按 index 重建块,`input_json_delta` 拼接后
    在 `content_block_stop` 解析成 `tool_use.input`。

- `upstream/anthropic.py`
  - `_build_payload` 转发 `tools` / `tool_choice`,存在 `content_blocks` 时
    原样发送。
  - `_map_event` 把 `content_block_start/_delta/_stop` 作为 PASSTHROUGH 中继
    (仍从 `message_start`/`message_delta` 抽 usage 供计费)。

## 测试

- `tests/test_tool_use_passthrough.py`(新增 4 项):入站保留 tools +
  tool_result 块;流式 tool_use 原样中继且无幽灵文本块;非流式重建 tool_use
  块(partial_json 正确解析);纯文本路径回归。
- `tests/test_codec.py` 5 项原有用例不变,全绿(9 passed)。
- 端到端 payload 校验:`tools` + `tool_choice` + `content_blocks` 确认进入
  上游请求体。

## 计费不变

usage 统计从不依赖 content 形状,仅读 `message_start`/`message_delta` 的
token,故本次改动对计费零影响。

## 待办

- 第二家具备工具能力的上游接入时,为 OpenAI 兼容上游扩展自有工具 schema。
- 给 `sandbox-runtime-verify.sh` 加端到端 tool-round-trip 检查
  (proof.txt + `--resume`),需 Docker + gateway + 真实上游 key 的活环境运行。
