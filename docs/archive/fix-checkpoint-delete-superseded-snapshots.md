# fix: Checkpoint 上传后删除同会话旧快照

- 变更时间：2026-07-12 10:07 (+08:00)

## 变更理由

Checkpoint 对象 key 包含时间戳和 UUID，每次保存都会在 MinIO 新增对象；数据库虽然只指向
latest checkpoint，但旧对象从未清理，长时间运行会持续积累无引用快照。

## 变更内容

- `apps/sandbox-manager/internal/provider/checkpoint/checkpoint.go`：新 checkpoint 上传并成功更新
  latest 指针后，列举同 user/session 前缀，删除除当前 key 外的全部旧对象；清理失败返回明确错误，
  但不回退已经可用的 latest 指针。
- `apps/sandbox-manager/internal/provider/checkpoint/checkpoint_test.go`：覆盖仅保留当前快照、不同 session
  隔离，以及单个删除失败后继续清理其余对象。
- `apps/agent-runtime/cocola_agent_runtime/objstore.py`、`checkpoint.py`：为 ReleaseSession 上传路径增加
  相同的按前缀清理；删除失败仅记录 warning，新 checkpoint 仍可恢复。
- `apps/agent-runtime/tests/test_server.py`：覆盖旧快照清理、跨 session 隔离和清理失败不破坏最新快照。

关键顺序固定为“上传新对象 → 更新数据库 latest 指针 → 清理旧对象”，不会在新快照尚不可恢复时
提前删除最后一个有效版本。不增加功能开关。
