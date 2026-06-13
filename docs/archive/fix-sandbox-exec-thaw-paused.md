# fix(sandbox-manager): exec 前自愈被 reaper 暂停的沙箱,消除 "context deadline exceeded"

Web 端复用一个空闲会话再次提问时,请求挂起约 60s 后报
`sandbox exec failed: context deadline exceeded`,模型无法应答。本次修复让
docker provider 在 exec 前检测并解冻被回收器暂停的沙箱。

## 根因

orchestrator 的两阶段回收(reaper)对空闲沙箱执行 stage-1 回收:租约
(LeaseTTL=60s)失效且无心跳时调用 `provider.Pause`,底层走 Docker 的
cgroup freezer 冻结容器(保留工作区,等待 stage-2 才 Destroy)。

心跳只在一次 query 进行中由 agent-runtime 发送。两次提问之间若超过租约,
沙箱即被冻结。当用户用**同一 session_id** 再次提问,binder 复用该沙箱并发起
exec——但 `Provider.Exec` 此前不会先解冻。对 freezer 冻结的容器创建 exec
进程会一直阻塞,直到调用方 deadline 到期,于是表现为 misleading 的
"context deadline exceeded"(实际是被冻住,不是网络/模型超时)。

## 改动

`apps/sandbox-manager/internal/provider/docker/docker.go`

- 新增 `thawIfPaused(ctx, cli, containerID, sid)`:inspect 容器,若
  `State.Paused` 则 `ContainerUnpause` 后再继续,并打一条
  `slog.Info("docker: thawed paused sandbox before exec")`。
  - inspect 失败**不**视为致命:吞掉错误,让下游 exec 给出权威错误
    (如 no-such-container),避免遮蔽真实原因。
  - 非暂停 / nil State 为 no-op。
- `Exec` 在 resolve、空命令校验之后、构造 execCfg 之前调用该 helper。
- 为可测试性,helper 依赖一个窄接口 `containerThawer`
  (`ContainerInspect` + `ContainerUnpause`),而非具体 `*client.Client`。

复用同一沙箱的 Resume(orchestrator 层)与这里的 exec 前解冻是互补的:前者
在分配/续租路径解冻,后者兜底处理"分配后、exec 前再次被回收器冻结"的竞态。

## 测试

`apps/sandbox-manager/internal/provider/docker/docker_test.go`(新增)
用 `fakeThawer` 覆盖五种情形:暂停→解冻一次、运行中→no-op、nil State→
no-op、inspect 出错→吞掉不解冻、unpause 出错→上抛。

- `go build ./... && go vet ./... && go test ./...`(go1.25 容器,
  GOWORK=off GOFLAGS=-mod=mod):sandbox-manager 全包通过。
- gofmt 干净。

## 端到端验收

`docker-compose.full.yml` 全栈 + 真实模型(COCOLA_LLM_PROVIDER=anthropic):

1. 经 gateway `/v1/chat` 发起首问创建新沙箱,模型回 "READY"。
2. 手动 `docker pause` 该沙箱容器(模拟 reaper stage-1),
   `State.Paused=true`。
3. 用同一 session_id 再次提问:返回 `reused=true`,模型回 "AWAKE"
   (修复前此处必然 deadline exceeded)。
4. 复核:容器回到 `running paused=false`;sandbox-manager 日志出现
   `docker: thawed paused sandbox before exec sandbox_id=sbx-...`。
