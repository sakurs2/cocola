# fix: OpenSandbox provider 真 server 端到端实测打通(4 个仅真 server 触发的缺陷)

日期: 2026-06-28
关联: ADR-0013、task #22、上游 commit 8f65aa5 / 7b3db4f / 35f951b

## 背景

OpenSandbox provider 在 P0–P2 阶段以 stub RoundTripper 完成了离线单测,但 ADR-0013 明确
要求「真 server 端到端实测」后才能将 Status 从 Proposed 转 Accepted。本次在本机
(macOS + OrbStack/Docker)启用真实 OpenSandbox server,通过一站式命令:

```
make verify-opensandbox-full           # 部署 server + 跑 harness
# 等价于:opensandbox-up 起 deploy/docker-compose/docker-compose.opensandbox.yml,
# 再 cd apps/sandbox-manager && GOWORK=off go run ./cmd/opensandbox-verify
```

跑通 Create→Health→Exec(流式)→文件往返→Pause→Resume→Destroy 全链路,最终输出
`VERIFY OK — all stages passed`。过程中暴露了 4 个只有对接真 server 才会触发的缺陷,
逐一定位并修复。

## 缺陷与修复

### 1. create 必须带非空 entrypoint
- 现象: `422 Entrypoint is required when image is provided`。
- 根因: OpenSandbox `CreateSandboxRequest` 在提供 image 时要求顶层
  `entrypoint: List[str]`(min_length=1);snapshot 模式服务端默认
  `["tail","-f","/dev/null"]`。
- 修复: `createSandboxRequest` 增 `Entrypoint []string`;`Create` 在 `spec.Image != ""`
  分支注入 `["tail","-f","/dev/null"]`。cocola 沙箱是「建一次、反复 Exec」的长生命
  周期模型,入口进程作空转阻塞,真实工作经 Exec 驱动——与服务端默认一致。

### 2. 有 image 时必须带 resourceLimits
- 现象: `422 resourceLimits is required when poolRef is not provided`。
- 根因: harness 未传 `-cpu/-mem`,`mapResources` 对零值不产出任何 limit。
- 修复: harness `-cpu` 默认 0→0.5、`-mem` 默认 0→512(provider 的 `mapResources`
  逻辑不变;0 仍表示「省略」)。

### 3. bridge 网络下 execd 端点需走 server proxy
- 现象: Exec 报 `host.docker.internal: no such host`。
- 根因: `GET /sandboxes/{id}/endpoints/{port}` 默认返回沙箱网络内地址
  (`host.docker.internal:PORT`),只有沙箱网络内的 client 能解析;harness 跑在
  宿主机上。
- 修复: `resolveExecd` 默认追加 `?use_server_proxy=true`,服务端返回
  `{server}/sandboxes/{id}/proxy/{port}` 这种「任何能连到 server 的 client 都可达」
  的代理 URL。新增 `WithServerProxy(bool)` 选项 + `COCOLA_OPENSANDBOX_DIRECT_EXEC`
  环境变量退回直连(仅当 sandbox-manager 与沙箱同网络时使用)。默认 true。

### 4. argv → execd 单条 shell 字符串的二次 shell 解析
- 现象: Exec 能跑但行为错乱——`sh -c "echo a; uname -a"` 只执行了 `echo`,退出码、
  多行输出、文件写入全部丢失。
- 根因: cocola `ExecRequest.Cmd` 是 argv(类似 docker exec),但 execd `/command`
  收的是单条 shell 字符串并会再用 shell 解析一遍。朴素 `strings.Join(cmd," ")` 会
  导致二次 shell:`["sh","-c","a; b"]` 被拼成 `sh -c a; b`,内层 sh 只拿到 `a`。
- 修复: 新增 `shellJoin`,对每个 argv 元素做单引号转义(`'\''` 惯用法,空串→`''`),
  execd 的 shell 重新解析后能精确还原原 argv。

## 实测结果

- 全链路 `VERIFY OK — all stages passed`;Exec 退出码透传正确(exit 3 → 3)、
  ~1MiB stdout 流式无损、文件写入/读回往返一致。
- Pause/Resume: Resume 调用 ~10ms 受理、~13ms 内回到 Running;Pause 前写入的标记
  文件在 Resume 后仍存在(状态保活)。这为 ADR-0013 / #15 的 RAM-kept resume 诉求
  提供了实测量级佐证。
- Destroy 干净退出。

## 测试

- 单测同步更新:`TestCreate_HappyPath` 断言新增 `"entrypoint":["tail","-f","/dev/null"]`;
  `TestExec_BridgesSSEStream` 期望从 `"command":"echo hi"` 改为 `"command":"'echo' 'hi'"`;
  新增 `TestShellJoin` 覆盖普通 / 含分号 / 含内嵌单引号 / 空串等 argv。
- `GOWORK=off go test ./...` 全绿,无回归。

## 影响面

- provider 行为变化对调用方透明(8 方法接口未动,ADR-0002 铁律不破)。
- 默认走 server proxy,对部署拓扑无新增要求;同网络部署可用环境变量退回直连以省一跳。
- ADR-0013 Status 据此从 Proposed 转 Accepted。
