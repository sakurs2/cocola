# feat: NFS host session storage

- 变更时间：2026-07-04 02:58 (+08:00)

## 变更理由

此前讨论过将 workspace 与 Claude 状态打包成 snapshot 上传对象存储，但该方案引入
checkpoint 打包、恢复、对象存储直传与失败重试等复杂主链路。当前阶段更希望保持
轻量：由运维把 NFS/NAS 挂载到宿主机固定路径，cocola 只负责把该路径映射给
sandbox。

最终目录模型收敛为：

```text
<COCOLA_SANDBOX_ROOT>/
  users/
    <safe_user_id>/
      sessions/
        <safe_session_id>/
          workspace/
          claude/
  plugins/
```

`workspace` 挂载到 `/workspace`，`claude` 挂载到
`/home/cocola/.claude`。两者都按 session 隔离；删除 conversation 时删除整个
session 目录，不影响其他对话。

## 变更内容

- `apps/sandbox-manager/internal/provider/docker`：Docker provider 使用新的
  `users/<user>/sessions/<session>` host 目录模型，并实现显式 session storage
  清理。
- `apps/sandbox-manager/internal/provider/opensandbox`：新增
  `COCOLA_SANDBOX_VOLUME_BACKEND=host`，OpenSandbox create 请求可使用 host
  volume backend 指向 `COCOLA_SANDBOX_ROOT` 下的 session 目录；保留默认 PVC
  backend 兼容旧路径。
- `apps/sandbox-manager/internal/orchestrator`：Release 显式删除会话时调用
  provider 的可选 session storage cleanup；idle reaper 仍只销毁 sandbox，不清理
  session storage。
- `apps/agent-runtime`：移除对象存储 checkpoint 打包/上传/恢复主链路，避免与 NFS
  session storage 双轨并存；数据库中的历史 checkpoint 字段仅作为兼容遗留字段保留。
- `deploy/docker-compose/docker-compose.opensandbox.yml`：把
  `COCOLA_SANDBOX_ROOT` 同路径挂进 OpenSandbox server，并配置
  `[storage].allowed_host_paths`，供 host volume backend 使用。
- `deploy/sandbox-runtime/README.md`、`deploy/docker-compose/docker-compose.full.yml`：
  更新当前目录模型说明。
- 单测覆盖 host volume 映射、路径 sanitization、Release 清理与 reaper 不清理。
