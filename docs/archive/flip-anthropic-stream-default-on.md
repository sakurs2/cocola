# Changelog: anthropic 上游流式默认翻回 ON

## 背景
`fix-anthropic-nonstream-fallback.md`(commit 见 ADR/plan)当时因中转 `aiberm.com` 这类 Anthropic 兼容上游的 SSE 流式接口坏掉(接受请求、HTTP 200,却不吐字节导致对话卡死),把 llm-gateway 的 `COCOLA_ANTHROPIC_STREAM` env 缺省值设成 OFF,真实对话走非流式 + 本地合成流事件。

## 变更缘由
用户实测上游流式输出已恢复正常,决定后续默认走真流式。

## 改动(纯 llm-gateway config 默认值翻转,无逻辑改动)
- `apps/llm-gateway/cocola_llm_gateway/config.py`
  - `_envflag(name, *, default=False)`:新增可选 `default` 形参,env 未设置/为空时返回该默认值(此前无 env 恒为 False)。其余调用点行为不变(仍默认 False)。
  - anthropic provider 的 `stream` 由 `_envflag("COCOLA_ANTHROPIC_STREAM")` 改为 `_envflag("COCOLA_ANTHROPIC_STREAM", default=True)`:env 缺省即真 SSE 流式。注释同步更新为"默认 ON,坏了置 0 回退"。
- `.env` / `.env.example`:此前已是 `COCOLA_ANTHROPIC_STREAM=true` 且注释已说明"默认流式",无需改动;现在代码默认与之一致。
- `upstream/anthropic.py` 的非流式回退路径与 codec 保持不变 —— 置 `COCOLA_ANTHROPIC_STREAM=0` 即可原样回退。

## 验证
- `ruff check config.py` 全绿。
- `.venv/bin/python -m pytest`(llm-gateway):115 passed, 3 skipped —— 与翻转前基线一致,零回归。
- `test_anthropic_nonstream.py` 5 项(含 stream=true 走 SSE / stream=false 走非流式)全过。

## 回滚
置 `COCOLA_ANTHROPIC_STREAM=0`(或配置文件 `stream: false`)恢复非流式回退。

## 关联
- 前置: docs/archive/fix-anthropic-nonstream-fallback.md
