# Plan: Route A shim resume 悬挂 id 的优雅降级

- 状态: Done(已实现)
- 日期: 2026-07-02
- 关联: ADR-0009(运行时进沙箱 / Route A)、ADR-0008(会话持久化与 --resume)
- 范围: agent-runtime 的 `InSandboxShimProvider` + `SessionMap`;不改协议、不改
  gateway/web、不改沙箱镜像。

## 1. 背景与现状

Route A 的多轮续聊靠 `session_map`(cocola `session_id` → `claude_session_id`)
把上一轮的会话 id 存下来,下一轮作为 `resume` 注入 shim 请求;shim 里
`claude_agent_sdk.query(resume=<id>)` 最终等价于 `claude --resume <id>`,从沙箱
持久卷上的 `~/.claude/projects/<proj>/<uuid>.jsonl` 重建大脑。

`session_map` 是一个**纯索引**:能真正续聊的**充分条件**是磁盘上的会话文件存在。
两者可能脱节:

- 沙箱是新建 / 被回收重建的(磁盘上没有该会话文件);
- 会话文件被 GC / 清理掉了;
- 索引写入过、但对应的磁盘态从未落地。

此时索引里的 id 变成**悬挂 id**。`claude --resume <悬挂id>` 退出非 0,shim
`main()` 捕获后发 `{"type":"error","stage":"run",...}` 并以 exit 1 收尾。
provider 侧就把它翻译成一句对用户毫无意义的 **"shim exited 1"**——一条本可以
正常回答的消息,因为一个陈旧索引而直接失败。

关键观察:**悬挂 resume 是在发出任何内容之前就失败的**(SDK 一启动就找不到会话
就抛错)。这让"重放本轮"成为安全操作——不会把已经吐给用户的半句话再吐一遍。

## 2. 目标

- 悬挂 resume 不再向用户暴露裸退出码;自动降级为**新开一轮**(不带 `--resume`),
  让用户拿到真实答复。
- 顺手把陈旧索引项清掉,避免下一轮再次踩雷。
- 严格限定重试触发条件,杜绝误重试导致的"答复翻倍"或掩盖真实错误。

## 3. 触发条件(三者同时满足才重试)

1. 本轮确实带了 `resume`(否则无所谓悬挂);
2. 本次 attempt 出错(`saw_error`)且**尚未吐出任何内容事件**(`not saw_content`);
3. 错误文本(shim error / stderr tail)命中 resume-not-found 特征串
   (`no conversation found` / `session id not found` / `session not found` …)。

只要吐过内容就**不重试**(避免重复答复);非 resume 类错误(如 CLI 缺失、exit 127)
也**不重试**,真实错误照常上抛。

## 4. 实现

### 4.1 `session_map.py`

- `SessionMap` Protocol 增 `delete(session_id)`;
- `MemorySessionMap.delete` = `dict.pop(..., None)`(幂等);
- `PostgresSessionMap.delete` = `DELETE FROM session_map WHERE session_id=%s`。

### 4.2 `shim_provider.py`

- 新增 resume-not-found 特征串常量 + `_looks_like_resume_not_found(text)`;
- 新增 `_AttemptState` dataclass:记录单次 attempt 的
  `saw_content / saw_error / error_text / last_session_id / errors`;
- 把原来单趟的流式循环抽成 `_stream_attempt(request_json, options, state)`:
  内容事件**实时 yield**,但把**终止错误(shim error / exec 传输错误 / 非0退出)
  延迟**记录到 `state`,不当场 yield——好让 `query` 在用户看到之前先决定要不要重试;
- `query` 编排:attempt-1(带 resume)→ 命中触发条件则 `delete` 陈旧索引 + attempt-2
  (不带 resume)→ 最终把**最后一次 attempt**的延迟错误上抛 → 写索引 → 合成唯一
  的终止 `done`。

事件顺序保持不变:内容 → (错误) → done;续聊 id 仍由 `done`/`result` 捕获并写回。

## 5. 测试

`tests/test_shim_provider.py` 新增:

- `test_dangling_resume_retries_fresh_and_reindexes`:第一趟带 stale resume + stderr
  "No conversation found" + exit 1 → 自动新开一轮 → 用户只看到 `text + done`,
  且索引被换成新 id;
- `test_unrelated_failure_is_not_retried`:exit 127 + "cli not found" → 不重试,
  `error + done` 照常上抛,旧索引不被清;
- `test_dangling_resume_not_retried_when_content_already_streamed`:已吐 `partial`
  后才出现 resume-ish 错误 → 不重试(`text + error + done`)。

`tests/test_session_map.py` 的契约测试增 `delete` 段(删除 + 幂等删未知)。

## 6. 验收

- `apps/agent-runtime/.venv/bin/python -m pytest`:72 passed, 2 skipped(PG 门控);
- `ruff check` / `ruff format --check`(改动文件)全绿。
