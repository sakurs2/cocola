# feat: session checkpoint restore

- 变更时间：2026-07-04 01:47 (+08:00)

## 变更理由

上一批已实现 sandbox 内 `tar | zstd | curl` 直传 checkpoint，但尚未登记 latest checkpoint，
也没有在新 sandbox 创建后恢复。为了支撑后续自由节点调度/迁移，需要先把 checkpoint 从“只上传”
推进到“可找到、可恢复”的最小闭环。

## 变更内容

- `db/migrations/00005_session_checkpoint.sql` 与 `00001_init_schema.sql`：为
  `session_map` 增加 `checkpoint_object_key`，记录每个 cocola session 的 latest
  checkpoint。
- `apps/agent-runtime/cocola_agent_runtime/session_map.py`：扩展 `SessionBinding` 与
  `SessionMap`，增加 `get_checkpoint` / `put_checkpoint`；checkpoint 更新不会覆盖
  Claude resume id。
- `apps/agent-runtime/cocola_agent_runtime/objstore.py`：增加 `presigned_get_url`。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：
  - checkpoint 上传成功后 best-effort 写入 latest checkpoint key；
  - fresh sandbox acquire 后，如果存在 checkpoint，使用预签名 GET URL 在 sandbox 内执行
    `curl | zstd -d | tar` 恢复；
  - 修正 checkpoint 归档路径，归档 `/workspace` 与 `/home/cocola/.claude`。
- `apps/agent-runtime/tests/*`：覆盖 checkpoint metadata、预签名 GET、fresh sandbox restore。

## 注意事项

- restore 仍为 best-effort：失败只记录 warning，不阻塞用户会话。
- 目前仅恢复 latest checkpoint，不做 checkpoint 列表、版本选择或 GC。
