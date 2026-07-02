# Changelog: opensandbox 卷属主修正(创建时一次性 chown)

日期:2026-07-02

## 症状

opensandbox 后端下:
1. Agent 写记忆文件报 “I wasn't able to write to the memory file due to a permissions issue”。
2. 告知名字后随即无法回忆,表现得像不支持连续对话。

## 根因

两个现象同一根因:`mapVolumes` 把新建的 per-user / per-session PVC 挂到
`/home/cocola/.claude`、`/data/userdata/<uid>`、`/workspace/<sid>`,这些卷首次
创建时属主为 root;而每次 Exec 都经 `runuser -u cocola` 降到非 root 的 cocola
(uid 10001),无法写入。`~/.claude` 写不进 → Claude Code 会话磁盘文件缺失 →
`--resume` 命中悬空会话,优雅降级为全新对话(“忘名字”)。

docker provider 靠宿主机 bind-mount 上 `os.Chown` 修属主;opensandbox 卷位于
server 运行时内部,sandbox-manager 触达不到,无法复用。

## 为何不用 securityContext / fsGroup

实时核对 `opensandbox/server:latest` OpenAPI schema:`CreateSandboxRequest` /
`Volume` / `PVC` 均无 `securityContext` / `fsGroup` / `runAsUser`;`extensions`
仅为不透明 `Dict[str,str]`。上游 server 不暴露该能力,该方案不可行。

## 变更

`apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go`

- 新增 `chownEntrypoint(execUser, uid, sid string) []string`:
  - `execUser != ""`(默认 cocola):
    `["/bin/sh","-c","chown -R '<u>':'<u>' '<.claude>' '<userdata>' '<workspace>' || true; exec sleep infinity"]`
  - `execUser == ""`(以 root 跑 Exec,无需修属主):`["sleep","infinity"]`
- `Create()` 把 `req.Entrypoint = []string{"tail","-f",...}` 替换为
  `chownEntrypoint(p.execUser, safe(spec.UserID), safe(spec.SessionID))`。
  entrypoint 以 root 运行(品牌镜像无 USER 指令),正好承载这一次性 chown,
  之后再 exec 空转进程。属主/路径经 `shellQuote` 转义,uid/sid 经既有 `safe()`
  规整,与 `mapVolumes` 完全一致。
- 空转进程由 `tail -f` 改为 `sleep infinity`(等价 idle-blocker,顺带清理)。

`apps/sandbox-manager/internal/provider/opensandbox/opensandbox_test.go`

- `TestCreate_HappyPath` entrypoint 断言更新为 `["sleep","infinity"]`
  (newStub 默认 `WithExecUser("")`)。
- 新增 `TestChownEntrypoint`:覆盖空 execUser(裸 blocker)、默认 execUser
  (chown 三路径 + `|| true` + `exec sleep infinity`)。

## 验证

- `cd apps/sandbox-manager && GOWORK=off go build ./... && go vet ./... && go test ./internal/provider/opensandbox/...` 全绿。
- 端到端(用户侧复验):新建会话 → 写记忆 → 报同名 → 复查记忆;预期两处恢复正常。

## 相关

- Plan: `docs/plan/opensandbox-volume-ownership-chown.md`
- 关联: `docs/archive/feat-opensandbox-volume-mapping.md`(卷映射)、
  `docs/archive/fix-opensandbox-route-a-e2e-chat.md`(Exec 降权 cocola)。
