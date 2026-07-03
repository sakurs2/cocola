# Plan：opensandbox 卷属主修正（创建时一次性 chown）

## 背景与症状

用户在 opensandbox 后端下遇到两个现象，实为同一根因：

1. Agent 写记忆文件时回答 “I wasn't able to write to the memory file due to a permissions issue”。
2. 用户告知名字 “jack”，随后 agent 无法回忆 —— 表现得像“不支持连续对话”。

连续对话机制本身是完备的（`session_id → claude_session_id` 索引 + `--resume`，hybrid 下由 Postgres `SessionMap` 持久化）。之所以“忘名字”，是因为 `~/.claude` 的 Claude Code 会话磁盘文件写不进去，`--resume` 命中悬空会话后优雅降级成全新对话。因此两个现象都指向同一处：**`~/.claude` 不可写**。

## 根因

opensandbox provider 的 `mapVolumes`（`opensandbox.go:895`）把新建的 per-user PVC 挂到 `/home/cocola/.claude`（subPath `.claude`）、`/data/userdata/<uid>`，把 per-session PVC 挂到 `/workspace`。这些 PVC 首次创建时属主是 **root**。而每次 `Exec` 都通过 `runuser -u cocola` 把权限降到非 root 的 `cocola`（uid 10001），于是 `cocola` 无法写入这些 root 属主的挂载点。

- docker provider 通过在**宿主机** bind-mount 目录上 `os.Chown` 修正属主（sandbox-manager 能直接摸宿主 FS）。
- opensandbox provider **无法复用**：其卷位于 OpenSandbox server 运行时内部，sandbox-manager 触达不到宿主路径。

## 为什么不用 securityContext / fsGroup（原“方案二”）

已用 `opensandbox/server:latest` 在 `:8090` 拉取实时 OpenAPI schema 确认：`CreateSandboxRequest`、`Volume`、`PVC` 均**无** `securityContext` / `fsGroup` / `runAsUser` 字段；`extensions` 仅为不透明的 `Dict[str,str]`，无法表达结构化 securityContext。旧的（已删除）K8s provider 能用 `fsGroup=10001` 是因为它直接生成 Pod spec；上游 server 不暴露该能力。**方案二在当前 server API 下不可行。**

## 方案（原“方案一”的非 trick 化收敛）

不在每次 Exec 前 chown（那才 trick），而是在**容器创建时一次性**把挂载点属主改成 exec 用户，然后再 exec 空转阻塞进程。

关键前提（均已核对）：
- 品牌镜像 `deploy/sandbox-runtime/Dockerfile` **无 `USER` 指令**，容器主进程以 root 运行（为让 firewall-entrypoint 能拿 NET_ADMIN）。opensandbox 路径下 cocola 已用自定义 entrypoint 覆盖镜像默认命令，因此 firewall-entrypoint 本就不跑，egress 由 OpenSandbox networkPolicy 接管 —— 这条链不受影响。
- cocola 现在把 opensandbox entrypoint 覆盖为空转阻塞进程（`opensandbox.go:349`）。entrypoint 以 root 运行 —— 正好可在这里做 chown。
- `chown` 的目标属主与 Exec 降权用户一致（`p.execUser`，默认 `cocola`）；`execUser==""`（即以 root 跑 Exec）时无需 chown。

### 新 entrypoint 形态

`execUser != ""`（默认）：

```
["/bin/sh","-c","chown -R <execUser>:<execUser> '/home/cocola/.claude' '/data/userdata/<uid>' '/workspace' || true; exec sleep infinity"]
```

`execUser == ""`（以 root 跑 Exec，无需修属主）：

```
["sleep","infinity"]
```

说明：
- 空转进程由 `tail -f`（旧）改为 `sleep infinity`，语义等价的 idle-blocker，同时消除对特殊设备路径的依赖，代码更干净（顺带清理）。
- `|| true` 保证个别路径缺失/已正确不致创建失败。
- 属主与路径都经 `shellQuote` 转义；uid/sid 经既有 `safe()` 规整（与 `mapVolumes` 完全一致）。

## 改动点

1. `apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`
   - 新增 `chownEntrypoint(execUser, uid, sid string) []string` 帮助函数（易测）。
   - `Create()` 中 `req.Entrypoint = []string{"tail","-f",...}`（~349）替换为 `req.Entrypoint = chownEntrypoint(p.execUser, safe(spec.UserID), safe(spec.SessionID))`。
   - 更新 entrypoint 处的注释，说明一次性 chown 的动机与 root/exec 用户的关系。

2. `apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`
   - 更新 `TestCreate_HappyPath`（~114）的 entrypoint 断言。
   - 新增 `TestChownEntrypoint`：覆盖默认 execUser（含 chown + 三路径 + exec sleep）、`execUser==""`（裸 sleep）、以及 uid/sid 被 `safe()` 规整的场景。

## 验证

- `cd apps/sandbox-manager && GOWORK=off go build ./... && GOWORK=off go test ./internal/provider/opensandbox/...`
- 用户侧端到端复验（无法由本 agent 端到端跑栈）：新建会话 → 让 agent 写记忆 → 报同名 → 复查是否记住；预期两处均恢复正常。

## 交付

- 全绿后写 `docs/archive/` changelog，提交（含 changelog，不含 `.claude/`，不 `--no-verify`）。
