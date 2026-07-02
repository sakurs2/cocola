# Fix: Anthropic 上游非流式回退(配置化 stream 开关)

## 症状
`--hybrid` 启动后与 AI 对话长时间无响应(转圈)。前两层修复(卷属主 chown 3ccb609、hybrid llm-gateway loopback 绑定 fc0f968)已生效,请求能到达网关,但对话仍卡住。

## 根因
网关配置的上游 `aiberm.com` 这类 Anthropic 兼容中转:**非流式接口正常,SSE 流式接口坏掉**(接受请求、返回 HTTP 200,却始终不吐字节)。宿主机压测对照:

| 模式 | 结果 | 首字节 |
|---|---|---|
| stream=false | HTTP 200,完整答案 | 3.8s |
| stream=true  | 45s 内 0 字节,连接挂死 | 从未返回 |

而 `upstream/anthropic.py` 的 `_build_payload` **硬编码 `"stream": True`**,每次真实对话都走流式,踩中坏路径 → 网关打开 SSE 连接(uvicorn 立刻记 200,是假象)→ 请求上游流式接口挂死 → 60s 后 httpx 超时 → `upstream timeout` 重试 → 客户端一直空等。

"之前测试 OK"是假象:HTTP 200 在流打开瞬间即记录,不代表模型回话;以往验证多为非流式/只看状态码,未覆盖流式真实路径。

## 改动
- `upstream/anthropic.py`
  - `AnthropicConfig` 增 `stream: bool = True`。
  - `_build_payload(req, *, stream)` 参数化 payload 的 `stream` 字段。
  - `chat_stream` 分流:`stream=True` 保持原 SSE 路径(零回归);`stream=False` 走新增 `_chat_nonstream`。
  - `_chat_nonstream`:非流式 POST 拿整包 Anthropic JSON,把 `content` 每个 block 重新发成下游 codec 已理解的 PASSTHROUGH `content_block_start/(delta)/stop` 帧(text 与 tool_use 都覆盖),再补 `message_start/message_delta/message_stop`;超时/4xx/坏 JSON 映射为 ERROR StreamEvent。**无需改 codec**。
- `config.py`:`_build_provider` 透传 `stream`;`_build_from_env` anthropic provider 读 `COCOLA_ANTHROPIC_STREAM`(env 缺省 = false,因当前上游流式坏)。
- `.env` / `.env.example`:新增 `COCOLA_ANTHROPIC_STREAM` 及说明。

## 验证
- 新增 `tests/test_anthropic_nonstream.py`(httpx MockTransport):stream=false 请求体、文本还原、tool_use 还原、超时/HTTP 错误映射、stream=true 仍走 SSE。
- `ruff check` 全绿;`pytest` 115 passed, 3 skipped。

## 回滚
置 `COCOLA_ANTHROPIC_STREAM=1`(或配置文件 `stream: true`)恢复原流式行为(适用于流式正常的上游)。

## 关联
- Plan: docs/plan/anthropic-nonstream-fallback.md
- 前置: 3ccb609(卷属主 chown)、fc0f968(hybrid loopback 绑定)
