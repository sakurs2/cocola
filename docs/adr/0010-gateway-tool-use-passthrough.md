# ADR-0010: Gateway tool-use passthrough (Anthropic rich-payload)

- Status: Accepted
- Date: 2026-06-11
- Deciders: @cocola-maintainers
- Supersedes the "M3 text-only relay" limitation noted in ADR-0004.

## Context

cocola 的 LLM gateway 暴露一个 Anthropic 兼容的 `POST /v1/messages` 前端,
因为这是 Claude Agent SDK 唯一能讲的线格式(ADR-0004, ADR-0009)。在 M3 里
gateway 被刻意做成了**纯文本中继**:为了尽快打通对话链路,归一化领域模型
(`types.py`)只承载纯文本消息,前端 codec **丢弃**了一切非文本结构:

1. `AnthropicRequest` 没有 `tools` / `tool_choice` 字段;`extra="ignore"`
   把它们静默丢弃。
2. `_flatten_content` 把 Anthropic content-block 数组
   (`text` / `image` / `tool_use` / `tool_result`)压成一个纯字符串。
3. Anthropic 上游的 `_build_payload` 从不转发 `tools`,`_parse_stream` 显式
   忽略 `content_block_start/stop`。

结果:沙箱内的 agent 让模型调用工具时永远收不到 `tool_use` 回复,因为工具
*定义*在到达上游之前就被剥掉了。这**不是** tool_use 难做的体现——任何开源
agent 代理只要转发 `tools` 字段就支持它。cocola 的阻塞点是 M3 简化自己造成的。

这一点已被实测证明:带 `tools` 的入站请求归一化后得到一个没有 tools 概念的
`ChatRequest`,因而上游 payload 里没有 `tools` 键。shim(ADR-0009)和上游代理
都没问题;缺口完全在 gateway 的 normalize/encode 层。

本 ADR 明确**不**处理:OpenAI 兼容上游的第二套 OpenAI 风格工具 schema(该适配器
暂时维持纯文本),以及超出 Anthropic 线格式已表达范围的并行多工具 fan-out 语义。

## Decision

通过 **Anthropic 富载荷的不透明透传**把 tool-use 端到端打通,让归一化层保持
厂商中立但无损。

具体地:

- **归一化类型(`types.py`)** 新增*附加的、可选的*字段,现有纯文本 provider
  不受影响:
  - `ChatMessage.content_blocks: list[dict] | None` —— 置位时,原始 Anthropic
    content-block 数组被原样保留(与现有的扁平化 `content: str` 并存,后者保留
    给计费的词数统计、以及只懂文本的 provider)。
  - `ChatParams.tools: list[dict]` 与 `ChatParams.tool_choice: dict | None` ——
    不透明的工具定义,原样转发。
  - `StreamEventType.PASSTHROUGH` —— 一个新事件,把上游 Anthropic 的
    content-block 帧(`content_block_start` / `content_block_delta` 含
    `input_json_delta` / `content_block_stop`)原样向下游中继。

- **入站 codec(`anthropic_codec.py`)** 把 `tools` / `tool_choice` 解析进
  `ChatParams`;当某条消息的 content 是含任何非文本块的块数组时,把原始数组存进
  `content_blocks`(仍把文本扁平化进 `content` 供计费)。

- **出站上游(`upstream/anthropic.py`)** 在 payload 里转发 `tools` /
  `tool_choice`,存在 `content_blocks` 时原样发送,并把 content-block 帧作为
  `PASSTHROUGH` 事件中继(同时仍从 `message_start` / `message_delta` 抽取 usage
  供计费)。

- **出站 codec(`anthropic_codec.py`)** 把 `PASSTHROUGH` 帧原样重新发出。发出
  legacy `CONTENT_DELTA` 的 provider(fake、OpenAI-compat)继续用合成的单一
  index-0 文本块;Anthropic 流通过 passthrough 自驱其块索引。这两种模式在单个
  响应内互斥,块索引绝不冲突。

## Alternatives Considered

- **Option A —— 完整归一化。** 把 tool_use/tool_result 建模为领域层一等的厂商
  中立结构,并让每个上游与之互译。否决:面积大,而当前唯一具备工具能力的上游
  只有 Anthropic,这套抽象买不到可移植性——我们会发明一个无法对第二家厂商
  验证的 schema。等 OpenAI 兼容上游需要工具时再重提。

- **Option B —— Anthropic 富载荷不透明透传。**(选中。)最小、无损、与开源代理
  既有做法一致;归一化层保持轻薄,改动是附加式的。

## Consequences

- **Positive** —— 沙箱内 agent 重获真正的 tool_use;gateway 不再是纯文本中继;
  计费/usage 统计不变,因为它从不依赖 content 形状;现有纯文本测试与 provider
  继续通过(字段是附加的)。

- **Negative / 已接受的风险** —— 归一化层现在承载一个 Anthropic 形状的不透明
  blob,厂商 schema 向本应中立的类型有小幅泄漏;OpenAI 兼容上游无法消费
  `content_blocks` 或 `tools`,会忽略它们(已记录);非流式 tool_use 由 passthrough
  帧按索引重建,比流式路径多一点代码。

- **Followups** —— 当第二家具备工具能力的厂商接入时,为 OpenAI 兼容上游扩展其
  自有工具 schema;给 `sandbox-runtime-verify.sh` 加一个端到端 tool-round-trip
  检查(proof.txt + `--resume`)。
