# refactor: session workspace storage model

- 变更时间：2026-07-04 01:20 (+08:00)

## 变更理由

旧目录模型同时保留 per-user writable volume、per-session workspace，以及独立
`/home/cocola/.claude` 挂载。这个模型会让同一用户的多个 cocola session 共享 Claude
Code config/state，不利于后续自由节点调度和 workspace checkpoint：打包或恢复一个
session 时可能碰到其它 session 的 `.claude` 状态。

本次先执行目录模型重构：所有可变 session 状态收敛到同一个 session 存储单元。
用户可见文件挂到 `/workspace`，Claude Code 状态挂到隐藏的
`/home/cocola/.claude`，两者都按 session 隔离。`/data/userdata/<user_id>` 不再保留，
workspace 配额与 sandbox 内直传对象存储 checkpoint 后续再实现。

## 变更内容

- `apps/sandbox-manager/internal/provider/docker/docker.go`：Docker provider 只挂载
  `/workspace`、session-local `/home/cocola/.claude` 与只读 `/data/plugins`，删除 user
  data 与 per-user `.claude` 挂载，并对 session 目录做 best-effort chown。
- `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`：OpenSandbox
  `mapVolumes` 改为使用同一个 session PVC 的 `workspace` / `claude` 两个 subPath；
  创建 entrypoint chown `/workspace` 与 `/home/cocola/.claude`。
- `deploy/sandbox-runtime/Dockerfile`：`CLAUDE_CONFIG_DIR` 与
  `ANTHROPIC_CONFIG_DIR` 指向 session-local `/home/cocola/.claude`，并补充 `zstd` 作为
  后续 checkpoint 打包基础工具。
- `apps/sandbox-manager/cmd/opensandbox-verify/main.go`、`scripts/*.sh` 与相关测试：
  验证同 session destroy/recreate 后 `/workspace` 与 `/home/cocola/.claude` 持久，且
  不同 session 不共享 workspace 文件。
- `docs/plan/*`、`docs/adr/*`、`deploy/sandbox-runtime/README.md`：同步当前生效的
  session workspace 模型，并标注旧双卷模型已被修订。

## 注意事项

- 本次没有实现 workspace 最大容量限制。
- 本次没有实现 workspace checkpoint 或 sandbox 内直传对象存储，只把目录边界先收敛到
  一个 session 存储单元下的 `workspace` 与 `claude` 两个子目录。
