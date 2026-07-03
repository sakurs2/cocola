# feat: session checkpoint direct upload

- 变更时间：2026-07-04 01:35 (+08:00)

## 变更理由

目录模型已收敛为一个 session 存储单元下的 `workspace` 与 `claude` 两个子目录。下一步需要
为未来自由节点调度/迁移准备高效 checkpoint 机制：打包应在 sandbox 内完成，归档应直接上传
对象存储，避免 agent-runtime 把整个工作区读进内存再转存。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/objstore.py`：为 MinIO fetcher 增加
  `presigned_put_url`，由后端生成短期 PUT URL，sandbox 不接触 MinIO 密钥。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：新增可选
  `COCOLA_SESSION_CHECKPOINT_ENABLED=1` 路径，在 agent turn 完成后执行 sandbox 内
  `tar | zstd | curl`，把 `/workspace` 与 `/home/cocola/.claude` 归档为
  `checkpoints/<user>/<session>/<ts>-<uuid>.tar.zst`。
- `apps/agent-runtime/tests/test_objstore.py`、`apps/agent-runtime/tests/test_server.py`：
  覆盖预签名 URL 与 checkpoint exec 调用。

## 注意事项

- v1 为 best-effort：checkpoint 失败只记录 warning，不影响用户回复。
- v1 默认关闭，需要设置 `COCOLA_SESSION_CHECKPOINT_ENABLED=1`。
- v1 暂不持久化 checkpoint metadata，也不做恢复流程；后续可在 session map 或独立表中登记
  latest checkpoint key。
