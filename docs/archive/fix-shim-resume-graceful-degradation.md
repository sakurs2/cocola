# fix(agent-runtime): Route A shim resume 悬挂 id 的优雅降级

## 问题

Route A 多轮续聊靠 `session_map`(cocola `session_id` → `claude_session_id`)存下上
一轮的会话 id,下一轮作为 `resume` 注入 shim;shim 里
`claude_agent_sdk.query(resume=<id>)` 等价于 `claude --resume <id>`,从沙箱持久卷上
的 `~/.claude/.../<uuid>.jsonl` 重建会话。

但 `session_map` 只是**索引**,能真正续聊的**充分条件**是磁盘上那份会话文件。两者会
脱节:沙箱新建/被回收重建、会话文件被 GC、或索引写过而磁盘态从未落地——此时索引里
的 id 变成**悬挂 id**。`claude --resume <悬挂id>` 退出非 0,shim 发
`{"type":"error","stage":"run",...}` + exit 1,provider 翻译成一句对用户毫无意义的
**"shim exited 1"**:一条本可正常回答的消息,仅因陈旧索引而失败。

关键点:悬挂 resume 在吐出任何内容**之前**就失败(SDK 一启动找不到会话即抛错),
所以"重放本轮"是安全的,不会把已吐给用户的半句话再吐一遍。

## 改动

### 1. `session_map.py`:新增 `delete`

- `SessionMap` Protocol 增 `delete(session_id)`;
- `MemorySessionMap.delete` = `dict.pop(..., None)`(幂等);
- `PostgresSessionMap.delete` = `DELETE FROM session_map WHERE session_id=%s`
  (新增 `_DELETE` SQL 常量)。

### 2. `shim_provider.py`:检测 + 单次新开重试

- 新增 resume-not-found 特征串常量 + `_looks_like_resume_not_found(text)`
  (匹配 "no conversation found" / "session id not found" / "session not found" 等)。
- 新增 `_AttemptState` dataclass:记录单次 attempt 的
  `saw_content / saw_error / error_text / last_session_id / errors`。
- 把原单趟流式循环抽成 `_stream_attempt(request_json, options, state)`:内容事件
  **实时 yield**,但把**终止错误(shim error / exec 传输错误 / 非0退出)延迟**记录到
  `state`,不当场 yield——好让 `query` 在用户看到之前先决定要不要重试。
- `query` 编排:attempt-1(带 resume)→ 若 **带过 resume + 出错 + 零内容 + 命中
  resume-not-found** 则 `delete` 陈旧索引 + attempt-2(不带 resume)→ 上抛**最后一次
  attempt** 的延迟错误 → 写索引 → 合成唯一终止 `done`。

事件顺序不变(内容 → 错误 → done);续聊 id 仍由 `done`/`result` 捕获写回。

### 触发条件(三者同时满足才重试)

1. 本轮带了 `resume`;2. 本次 attempt 出错且**尚未吐出任何内容**;
3. 错误文本命中 resume-not-found 特征。任一不满足即不重试:已吐内容 → 不重试(防
答复翻倍);非 resume 类错误(如 exit 127 CLI 缺失)→ 不重试,真实错误照常上抛。

## 测试

- `tests/test_shim_provider.py` 新增 3 例:
  - `test_dangling_resume_retries_fresh_and_reindexes`:stale resume + "No
    conversation found" + exit 1 → 自动新开一轮 → 用户只见 `text + done`,索引换成新 id;
  - `test_unrelated_failure_is_not_retried`:exit 127 → 不重试,`error + done` 照常上抛,
    旧索引不被清;
  - `test_dangling_resume_not_retried_when_content_already_streamed`:已吐 `partial`
    后才报 resume-ish 错误 → 不重试(`text + error + done`)。
- `tests/test_session_map.py`:契约测试增 `delete` 段(删除 + 幂等删未知)。

## 验收

- `apps/agent-runtime/.venv/bin/python -m pytest`:72 passed, 2 skipped(PG 门控);
- `ruff check` / `ruff format --check`(改动文件)全绿。

## 关联

Plan: `docs/plan/fix-shim-resume-graceful-degradation.md`;ADR-0009(Route A)、
ADR-0008(会话持久化与 --resume)。
