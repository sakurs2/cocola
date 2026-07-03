# Plan: OpenSandbox workspace volume ownership fix

## 背景与症状

OpenSandbox provider 创建的 PVC 首次挂载时通常归 root 所有，而 cocola 执行 agent
命令时会通过 `runuser -u cocola` 降权到非 root 用户(uid 10001)。如果不修正属主，
agent 无法写 `/workspace` 和 `/home/cocola/.claude`。

## 当前目录模型

可写状态只保留在 session workspace:

- `/workspace`
- `/home/cocola/.claude`

只读平台资源:

- `/data/plugins`

不再挂载 `/data/userdata/<userID>`，且 `/home/cocola/.claude` 不再来自 per-user volume，
而是同一 session PVC 的 `claude` subPath。

## 方案

不在每次 Exec 前 chown，而是在容器创建时一次性把 session 挂载点属主改成 exec 用户，
并确保 `/workspace` 与 `/home/cocola/.claude` 存在，然后进入空转阻塞进程。

`execUser != ""`（默认 `cocola`）：

```json
["/bin/sh","-c","mkdir -p '/workspace' '/home/cocola/.claude' && chown -R <execUser>:<execUser> '/workspace' '/home/cocola/.claude' || true; exec sleep infinity"]
```

`execUser == ""`（以 root 跑 Exec，无需修属主）：

```json
["sleep","infinity"]
```

## 验证

- `TestChownEntrypoint` 覆盖 root/no-root 两种分支。
- OpenSandbox live verify 写入 `/home/cocola/.claude/marker.txt` 成功。
- 同 session destroy/recreate 后 `/workspace` 数据可读回。
