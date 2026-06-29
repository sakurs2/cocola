# fix: OpenSandbox 后端 Route A 端到端 chat 跑通(stdin/非root/就绪/流式)

日期：2026-06-29 · 关联 ADR：0009(Route A)/0013/0015

## 背景
`COCOLA_SANDBOX_PROVIDER=opensandbox` 全栈下做 `/v1/chat` 端到端验收:在沙箱内
拉起 claude(Route A),返回真实模型答案(目标串 `cocola-e2e-ok`)。验收过程中逐层
暴露并修复了 5 个 bug,最终 chat 稳定返回 `text` + `result` 事件,内容为
`cocola-e2e-ok`。

## 改动

### 1. metrics_grpc 协程式 server-streaming 拦截器(packages/py-common)
agent-runtime 的 `Query` 是 **协程**(通过 `context.write()` 流式、返回 None),
不是 async generator。原拦截器只按 async generator 包装(`async for` 需 `__aiter__`),
对协程式 handler 直接报错,整条 chat 链路在 agent-runtime 入口即失败。
- `_wrap_unary_stream` 用 `inspect.isasyncgenfunction` 判别 handler 形态:
  async generator → `gen_wrapper`(沿用 yield 转发);协程 → `coro_wrapper`
  (`await behavior` 后保留返回值,指标记账逻辑一致)。
- 新增两条单测覆盖协程式 handler 的 OK / 异常(UNKNOWN)记账。

### 2. opensandbox Exec:stdin 经 base64 管道注入(apps/sandbox-manager)
execd `/command` API 无 stdin 字段(spec 仅 command/cwd/envs/timeout/uid/gid),
而 Route A shim 必须从 stdin 读取一次性 Request JSON。
- 将 stdin `base64` 编码(二进制安全、字母表无需 shell 转义),在 shell 内
  `printf %s '<b64>' | base64 -d | <command>` 解码后管道注入命令 stdin。
- 管道前置于 runuser 包装之后,字节流经 runuser 转发给目标进程,扁平单管道
  (无嵌套 `bash -c`,规避 "unexpected EOF" 解析错误)。

### 3. opensandbox Exec:以非 root 用户 cocola 运行(apps/sandbox-manager)
execd 默认以 root 运行 `/command`,而 in-sandbox claude CLI 在 root 下拒绝
`--dangerously-skip-permissions`。
- `runuser -u cocola -- <argv>` 降权(直接执行 argv、保留环境含注入的
  ANTHROPIC_* 凭据、设置 HOME),对齐 docker provider 的 `docker exec --user cocola`。
- 默认用户 `sandboxExecUser="cocola"`(uid 10001),可经
  `COCOLA_OPENSANDBOX_EXEC_USER` 覆盖(空 / "root" 关闭降权)。

### 4. opensandbox Exec:execd 冷启动就绪竞争(apps/sandbox-manager)
新建/resume 的容器在 execd 绑定监听 socket 之前就报 Running,该窗口内的 exec
会被 server proxy 以 500 "Server disconnected without sending a response" 拒绝。
- `thawIfPaused`:exec 前若沙箱 Paused 则 resume 并等待 Running(对齐 docker
  provider;ADR-0015 按需分配模型下,后续每一轮都会先 resume)。
- `waitExecdReady`:resolveExecd 后用幂等 `true` 探针轮询,直到 execd 返回 2xx
  才发真实命令;5xx/dial 失败视为未就绪重试,4xx 视为真错误,
  `execReadyTimeout=30s` / `execReadyPollInterval=300ms` 兜底。

### 5. opensandbox Exec:流式截断 + stdout 换行帧丢失(apps/sandbox-manager)
就绪修复后 chat 仍返回空(只有 sandbox+done,无 text)。两处根因:
- **流式被 keep-alive 复用截断**:`waitExecdReady` 探针与真实命令复用同一
  连接池连接时,server proxy 会在 ~1s 后截断真实命令的 SSE 流(只见一个空
  EXIT)。**stream client 关闭 keep-alive**(`DisableKeepAlives:true`),每次
  exec 新建连接,规避 proxy 连接状态 bug(exec 本就分钟级,建连开销可忽略)。
- **stdout 换行帧丢失**:execd 按行缓冲子进程 stdout,每行一个 event 且**去掉
  行尾换行**;下游 agent-runtime shim_provider 按 `\n` 重组 NDJSON,无分隔符
  导致 JSON 对象粘连、全部解析失败(空输出)。`processSSEPayload` 对 stdout
  事件**归一化补回一个行尾换行**,使 opensandbox 与 docker(原生保留换行)
  呈现一致的换行分隔字节流。

## 校验
- `GOWORK=off go test ./...`(sandbox-manager)全绿;新增单测
  `TestExec_StdoutNewlineFraming`(换行帧)、`TestExec_WaitsForExecdReady`
  (就绪重试)通过。
- `uv run pytest`(agent-runtime)50 passed / 2 skipped;含新增协程式拦截器单测。
- 端到端:`POST /v1/chat`(provider=opensandbox,debug-free 镜像)返回
  `event: text … cocola-e2e-ok` 与 `event: result … "result":"cocola-e2e-ok"`。

## 非目标
- 不做镜像瘦身(用户明确"先不做瘦身")。
- 不实现 opensandbox WriteFile/ReadFile(仍 errNotImplemented,另案)。
