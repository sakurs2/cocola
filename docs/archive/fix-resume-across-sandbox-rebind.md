# fix: resume across sandbox rebind

- 变更时间：2026-07-05 00:09 (+08:00)

## 变更理由

同一个 cocola 会话在沙箱回收/重建后可能绑定到新的 sandbox_id，但会话的持久化卷仍然保留 Claude 的 on-disk session 文件。此前 agent-runtime 在发现 `session_map` 记录的 sandbox_id 与当前 sandbox_id 不一致时，会直接删除旧 resume id，导致用户回到同一会话后无法继续上文。

## 变更内容

- apps/agent-runtime/cocola_agent_runtime/shim_provider.py：sandbox_id 变化时不再提前删除旧 resume id，改为继续尝试已记录的 Claude session；只有真正出现 dangling resume 错误时才按原有逻辑删除并 fresh retry。
- apps/agent-runtime/tests/test_shim_provider.py：更新回归测试，覆盖 sandbox_id 变化时仍携带旧 `resume`，成功后把 binding 更新到新 sandbox_id。
- 关键取舍：resume 是否有效以沙箱内 Claude session 文件是否存在为准，而不是以 sandbox_id 是否一致为准。
