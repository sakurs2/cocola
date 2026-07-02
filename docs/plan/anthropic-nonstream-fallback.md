# Plan: Anthropic 上游非流式回退(配置化)

## 背景 / 症状

`--hybrid` 启动后与 AI 对话长时间无响应(转圈)。前两层已修复并生效:
- 卷属主 chown(commit 3ccb609):`.claude`/`userdata`/`workspace` 归 cocola。
- hybrid llm-gateway loopback 绑定(commit fc0f968):8081 改绑 0.0.0.0,请求已能从沙箱容器到达网关。

第三层为本 Plan 的目标。宿主机对照压测配置的上游 `https://aiberm.com`:

| 模式 | 结果 | 首字节 |
|---|---|---|
| `stream=false` | HTTP 200,返回完整正确答案 | 3.8s |
| `stream=true`  | 45s 内 0 字节,连接挂死超时(curl 28) | 从未返回 |

## 根因

上游 `aiberm.com` 的 **非流式 Messages 接口正常,SSE 流式接口坏掉**(不吐字节)。
而 `upstream/anthropic.py` 的 `_build_payload` **硬编码 `"stream": True`**,每次真实对话都走流式,恰好踩坏路径:
网关打开 SSE 连接(uvicorn 立刻记 `200 OK`,是假象)→ 请求上游流式接口挂死 → 60s 后 httpx 超时 → 打印 `upstream timeout` → 重试 → 客户端一直空等。

"之前测试 OK"是假象:HTTP 200 状态行在流打开瞬间即记录,不代表模型回话;且以往验证多为非流式/只看状态码,未覆盖流式真实路径。

## 目标

给 `AnthropicUpstream` 增加 **stream 开关**,支持"非流式回退":
- `stream=False` 时:`POST /v1/messages` 带 `"stream": false`,拿整包 Anthropic JSON,在本地**合成**下游 codec 期望的 `StreamEvent` 序列(对下游透明,依旧表现为流式)。
- `stream=True` 时:保持现有 SSE 行为,零回归。
- 通过 env `COCOLA_ANTHROPIC_STREAM` / 配置文件 `stream` 字段配置化。
- 当前上游流式坏,默认值设为 **false**(可随时切回)。

## 下游契约(已核实)

codec 的 `stream_to_anthropic_sse` 与 `collect_to_anthropic_response` 都消费统一的 `StreamEvent`:
- `MESSAGE_START(usage=input_tokens, model)`
- 文本块:走 `PASSTHROUGH` content-block 帧(`content_block_start(text)` → `content_block_delta(text_delta)` → `content_block_stop`),或直接 `CONTENT_DELTA`。
- 富块(tool_use):`PASSTHROUGH` 帧(`content_block_start(tool_use)` → `content_block_delta(input_json_delta)` → `content_block_stop`),index 由上游给。
- `MESSAGE_DELTA(usage=output_tokens, finish_reason=stop_reason)`
- `MESSAGE_STOP`

**关键决策**:非流式响应体已含完整 `content: [block...]` 数组与 `usage`。把每个 block 按其类型重新发成 `PASSTHROUGH content_block_start/(delta)/stop` 帧,即可复用现有 codec 的富块重建逻辑(text 与 tool_use 都覆盖),**无需改 codec**。

## 变更点

1. `upstream/anthropic.py`
   - `AnthropicConfig` 加 `stream: bool = True`(保守默认 true;实际默认值由 config 层按 env 决定)。
   - `_build_payload` 的 `"stream"` 改为读 `req`/cfg 决定 —— 采用:构造时记住 `self._stream`,`_build_payload` 接收 stream 参数。
   - `chat_stream`:`self._stream` 为 True 走现有 `_parse_stream`;为 False 走新 `_chat_nonstream`。
   - 新增 `_chat_nonstream`:非流式 POST,`resp.json()`,合成 StreamEvent:
     - `MESSAGE_START`(input_tokens、model)
     - 遍历 `content`:按 block index 发 `PASSTHROUGH content_block_start`;text 块补一条 `content_block_delta(text_delta)`,tool_use 块补一条 `content_block_delta(input_json_delta, partial_json=json.dumps(input))`;再发 `content_block_stop`。
     - `MESSAGE_DELTA`(output_tokens、stop_reason)、`MESSAGE_STOP`。
     - 错误/超时:与流式路径一致映射为 ERROR StreamEvent(code=timeout/transport/upstream_http_error)。

2. `config.py`
   - `_build_provider` anthropic 分支透传 `stream=bool(cfg.get("stream", ...))`。
   - `_build_from_env` anthropic provider dict 增 `"stream"`,由 `_envflag("COCOLA_ANTHROPIC_STREAM")` 决定;默认 false。

3. `.env` / `.env.example`:新增 `COCOLA_ANTHROPIC_STREAM=false` 及说明(上游流式接口异常时置 false)。

## 验证

- 新增 `tests/test_anthropic_nonstream.py`(httpx MockTransport):
  - stream=False 时请求体 `"stream": false`。
  - 纯文本响应 → 合成事件序列正确、`collect_to_anthropic_response` 能还原文本、usage 正确。
  - tool_use 响应 → PASSTHROUGH 帧能被 codec 重建为 tool_use block(input 正确)。
  - 超时 → ERROR(code=timeout);4xx → upstream_http_error。
- `ruff check` + `pytest` 全绿。
- 端到端:用户 `--hybrid` 起栈,置 `COCOLA_ANTHROPIC_STREAM=false`,对话应得到回复。

## 回滚

置 `COCOLA_ANTHROPIC_STREAM=true` 即恢复原流式行为(适用于流式正常的上游)。
