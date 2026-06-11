# test(tool-use): 补齐 tool-use 验证工具链 (ADR-0010)

承接 0dd90a7(tool-use 透传修复),补齐三层验证工具,让 tool-use 可被完整验证。

## 三层验证工具

| 工具 | 层级 | 依赖 | 验证什么 |
|---|---|---|---|
| `FakeUpstream(tool_call=...)` | 测试基建 | 无 | 让 hermetic 测试能模拟模型发起 tool_use |
| `scripts/llm-tooluse-e2e.py` | gateway 全链路(hermetic) | 仅 Python | tools 透传 + tool_use SSE + 非流式重建 + 计费 |
| `scripts/llm-tooluse-probe.sh` | 活体 HTTP | 运行中的 gateway + 真实上游 | 真 curl 看真实上游的 tool_use SSE |
| `sandbox-runtime-verify.sh`(强化) | Docker 沙箱端到端 | Docker + gateway + key | 沙箱内 Bash 工具落地 = tool_use 跑通 |

## 改动

- `upstream/fake.py`:新增可选 `tool_call` 参数。置位时 FakeUpstream 改为吐
  Anthropic 形状的 PASSTHROUGH 帧(content_block_start[tool_use] + 分块
  input_json_delta + content_block_stop + stop_reason=tool_use),不传时行为
  完全不变(向后兼容)。计费沿用"chunk 数 = completion_tokens"启发式。

- `scripts/llm-tooluse-e2e.py`(新增):hermetic 端到端,经真实 ASGI HTTP 打
  `/v1/messages`(带 tools),断言 4 件事——
  1. STREAM:SSE 含 tool_use 块 + input_json_delta,且无幽灵文本块;
  2. COLLECT:非流式 JSON 重建 tool_use 块,args 从 partial_json 拼回;
  3. PASSTHRU:client 发的 `tools` 确实到达上游(RecordingFake 捕获);
  4. BILLING:计费一次且 token 非零。
  无需 Docker、无需 key。

- `scripts/llm-tooluse-probe.sh`(新增):活体 curl 探针,打运行中的 gateway,
  强制一个会触发工具调用的 prompt,检查响应里有没有 tool_use / input_json_delta。
  支持 STREAM=1/0、自定义 BASE_URL/MODEL/TOKEN。

- `scripts/sandbox-runtime-verify.sh`(强化):step 3 的 proof.txt 检查本就是
  沙箱内 tool-use 活体测试(模型必须调 Bash 工具才能写文件);新增对 shim
  NDJSON 流里 tool_use/tool 事件的显式断言,并修正 header 里过时的 MODEL
  默认值注释(claude-3-5-sonnet → cocola-default)。

- `tests/test_fake_upstream.py`:新增 2 项——tool_use 模式发出正确 PASSTHROUGH
  帧;text 模式不受 tool 支持影响(回归)。

## 测试结果

- gateway 单测 21 passed(含新增 6 项 tool-use 用例)。
- `llm-tooluse-e2e.py`:4/4 全过。
- ruff check / format 全绿。

## 你能怎么验证(三档,从快到全)

1. 秒级 / 无依赖:
   `cd apps/llm-gateway && uv run python ../../scripts/llm-tooluse-e2e.py`
2. 活体上游:`run-stack.sh --with-llm`(配真实上游)后
   `TOKEN=<dev-token> bash scripts/llm-tooluse-probe.sh`
3. 全沙箱:`scripts/sandbox-runtime-verify.sh`(需 Docker + gateway 环境)。

## 何时该跑 sandbox-runtime-verify(与前 3 档的分工)

前 3 档(单测 / hermetic e2e / live probe)用 FakeUpstream 或薄链路证明
**网关编解码层逻辑正确**:tool_use 帧透传、非流式重建、tools 抵达上游、
计费独立。它们秒级、零(或轻)依赖,是平时回归的主力。

`sandbox-runtime-verify.sh` 是**唯一证明真实端到端链路**的一档:Route-A
容器内的 Claude CLI 经 shim → 网关 → 真模型,真的发出 tool_use 才能写出
`proof.txt`——若工具字段被丢,文件不会出现。代价是重依赖(本机 Docker +
构建 sandbox 镜像 + 真实 ANTHROPIC_BASE_URL/AUTH_TOKEN)。

因此**不必每次回归都跑**,只在以下两个时机跑一次留证据:

1. 改动 sandbox 镜像 / shim / 网关 egress 相关代码时;
2. 把 ADR-0010 收口为"真上线可用"前的验收。

平时改网关 codec 逻辑,跑前 2 档即可。
