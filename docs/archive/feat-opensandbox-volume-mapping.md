# Changelog: OpenSandbox provider 接入双卷文件系统(volumes 映射)

日期: 2026-06-28
关联: task #26、ADR-0008、ADR-0014、ADR-0002、docs/plan/opensandbox-volume-mapping.md

## 背景
opensandbox provider 之前 Create 不传任何 volume,ADR-0008 的「用户持久 / 会话工作区 /
系统只读 / 默认不持久」语义完全缺失。本次按 Plan 把双卷模型落到 OpenSandbox `volumes`。

## 改动
### apps/sandbox-manager/internal/provider/opensandbox/opensandbox.go
- 新增 guest 路径常量(与 docker provider / brain 镜像契约一致):
  guestUserData、guestWorkspace、guestPlugins、guestClaudeConfig、claudeSubPath、
  pluginsClaimName。
- `createSandboxRequest` 增 `Volumes []volumeSpec`;新增 wire 类型 `volumeSpec`
  (pvc/mountPath/readOnly/subPath)、`pvcBackend`
  (claimName/createIfNotExists/storage/storageClass/accessModes/deleteOnSandboxTermination)。
- 新增 `mapVolumes(userID, sessionID)`:生成 4 个卷 ——
  - 用户文件(T2):pvc `cocola-user-<uid>`,createIfNotExists @ /data/userdata/<uid>
  - Claude 状态(T2):同一用户卷 subPath=.claude @ /home/cocola/.claude
  - 会话工作区(T1b):pvc `cocola-session-<sid>`,createIfNotExists,
    不 deleteOnSandboxTermination(cocola 编排层 GC)@ /workspace/<sid>
  - 平台 skill:共享 pvc `cocola-plugins`,readOnly @ /data/plugins
- `Create` 调用 `mapVolumes` 把卷写到请求体。
- 新增 `safe()`:把 cocola id sanitize 成 OpenSandbox 合法卷名(DNS-label 规则,
  `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`,空值回退 "x")。
- 更新包级 doc 说明 Create 现在映射双卷模型。

### opensandbox_test.go
- 新增 `TestSafe`(含非 ASCII、空值回退,并校验拼出的 claim 名合法)、
  `TestMapVolumes`(4 卷断言 + .claude 复用用户卷 claim)、
  `TestMapVolumes_SanitisesIDs`、`TestCreate_SendsVolumes`(卷落到 wire)。

### cmd/opensandbox-verify/main.go
- 新增 `-persist` flag 与 Stage 6 `verifyPersistence`:用固定 user/session,
  create A -> 向用户卷/.claude(subPath)/会话工作区写 marker -> destroy A ->
  同 id create B -> 读回三个 marker,验证 T2/.claude/T1b 跨 destroy-recreate 持久化,
  并顺带验 ~/.claude 的 uid 写权限。仅真 server 运行。

## 验收
- `GOWORK=off go build ./...` 绿;`go test ./...` 绿(opensandbox 包含新单测全过)。
- 待真 server:`go run ./cmd/opensandbox-verify -persist` 跑通跨 destroy-recreate 持久化
  + uid 写权限;并确认 Docker named volume 多 subpath 挂载可用(Plan 实测子项)。

## 不含
ossfs、snapshot resume、docker provider 改动、WriteFile/ReadFile 实现(execd upload/download,
单列后续任务)。
