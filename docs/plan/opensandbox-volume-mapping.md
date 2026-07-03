# Plan: OpenSandbox session-workspace volume mapping

日期: 2026-07-04
关联: ADR-0008(持久化分层)、ADR-0014(OpenSandbox 主力)、ADR-0002(provider 接口)
状态: 已修订落地

## 目标

把 cocola 的沙箱目录模型收敛为“每个 session 一个可持久化 workspace”，并在
Docker provider 与 OpenSandbox provider 之间保持同一套容器内路径契约。

当前模型:

| 容器内路径 | 后端映射 | 语义 |
|---|---|---|
| `/workspace` | session volume/PVC `cocola-session-<sessionID>` 的 `workspace` subPath | agent 工作区、上传文件、输出文件 |
| `/home/cocola/.claude` | 同一 session volume/PVC 的 `claude` subPath | Claude Code config / session files, 每个 cocola session 独立且不暴露在工作区 |
| `/data/plugins` | shared volume / PVC `cocola-plugins`, read-only | 平台预置 skill / plugin |

不再保留:

- `/data/userdata/<userID>`
- `cocola-user-<userID>` PVC / volume
- per-user `.claude` 挂载

## 设计理由

之前的 per-user volume 让多个 cocola session 共享同一个 Claude config，后续如果要做
自由节点调度和 workspace checkpoint，会带来两个问题：

1. `.claude` 打包/恢复时可能与同用户其它 session 的状态冲突。
2. session workspace 与 user volume 生命周期不同，迁移和 GC 边界不清晰。

因此 v1 收敛为：**所有可变 session 状态都在同一个 session 存储单元下**。用户可见文件
放在 `workspace` subPath，Claude Code 状态放在隐藏的 `claude` subPath。这样未来
checkpoint 可以围绕同一个 session volume 做增量打包、上传、下载和恢复，同时避免用户
询问文件列表时看到 `.claude`。

## OpenSandbox volume mapping

`mapVolumes(sessionID)` 生成三个 volume:

| Name | PVC | mountPath | readOnly |
|---|---|---|---|
| `session` | `cocola-session-<safe(sessionID)>`, `subPath=workspace`, `createIfNotExists=true` | `/workspace` | false |
| `claude` | 同一 `cocola-session-<safe(sessionID)>`, `subPath=claude`, `createIfNotExists=true` | `/home/cocola/.claude` | false |
| `plugins` | `cocola-plugins` | `/data/plugins` | true |

`cocola-session-*` 不设置 `deleteOnSandboxTermination`，由 cocola 生命周期管理负责清理。

## 权限

OpenSandbox 首次创建 PVC 时挂载点通常为 root 所有，而 Exec 会通过 `runuser -u cocola`
降权到 uid 10001。provider 因此在 sandbox entrypoint 中执行一次：

```sh
mkdir -p /workspace /home/cocola/.claude && chown -R cocola:cocola /workspace /home/cocola/.claude || true
exec sleep infinity
```

Docker provider 则在宿主机上对 `<root>/workspace/<sessionID>/workspace` 和
`<root>/workspace/<sessionID>/claude` 做 best-effort `chown`。

## 验收

- OpenSandbox create 请求包含 `/workspace`、`/home/cocola/.claude` 和 `/data/plugins`。
- `/home/cocola/.claude` 可由 `cocola` 用户写入，但不在 `/workspace` 文件列表中。
- 同一 session destroy/recreate 后 `/workspace` 与 `/home/cocola/.claude` 数据仍在。
- 不同 session 之间不可见彼此 workspace 文件。
