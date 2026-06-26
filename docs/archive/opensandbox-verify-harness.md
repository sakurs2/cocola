# OpenSandbox provider 手动验证程序(cmd/opensandbox-verify)

- 关联:#22(真 server 端到端验证)、ADR-0013、`docs/archive/opensandbox-poc-p2-exec-pause-resume.md`
- 日期:2026-06-26
- 性质:手动验证工具(非单测、非运行时数据路径)

## 目标

P0–P2 已在离线环境用 RoundTripper/SSE stub 完成单测,但真 server 端到端
(OpenSandbox server 监听端口 + 每个 sandbox 内 execd 监听)在当前合规沙箱里
跑不了。本程序把完整生命周期打包成一个 `go run` 即可执行的 harness,交给可在
本机起监听进程的环境跑,用真实数据决定:

- Exec 流式事件保真度(stdout/stderr 顺序、分块、退出码、大输出不截断);
- Pause→Resume 延迟(支撑 #15 RAM-kept resume 决策);
- Pause/Resume 后进程内文件状态是否存活;
- 据此把 ADR-0013 的 Status 从 Proposed 翻成 Accepted 或修订。

## 改动

新增 `apps/sandbox-manager/cmd/opensandbox-verify/main.go`(单文件,仅依赖
`internal/provider` 与 `internal/provider/opensandbox`,不经 gRPC/sandbox-manager,
直连 provider 以隔离 provider<->OpenSandbox 这一道缝)。

行为:
1. `opensandbox.New()` 从 env 读 `COCOLA_OPENSANDBOX_URL` /
   `COCOLA_OPENSANDBOX_API_KEY`;URL 缺失时立即 fatal 并给出 export 提示。
2. Create(可选 -image/-cpu/-mem/-egress)→ 轮询 Health 到 Running。
3. Exec 流式矩阵:基础 stdout、stderr 捕获、非零退出(exit 3)、env+cwd、
   ~1MiB 大 stdout;逐事件原样打印,末尾校验退出码。
4. 经 shell exec 做文件写-读往返(WriteFile/ReadFile 在 PoC 中仍 deferred,
   故用 exec 证明运行时实际依赖的文件 IO 能力)。
5. Pause → 打印 Health → Resume,测量"resume 被接受"与"重新回到 Running"
   两个延迟;再 cat 之前写的 marker 文件,验证状态跨 pause/resume 存活。
6. 结束统一汇总:全过则 `VERIFY OK` 退出 0,任一阶段失败则列出失败阶段、
   `VERIFY FAIL` 退出 1。

flags:`-image -cpu -mem -egress -timeout -keep -skip-pause`。
`-keep` 保留 sandbox 供手动排查;`-skip-pause` 应对暂不支持 pause 的 runtime。
无论成败都在 defer 里尽力 Destroy(除非 -keep)。

## 验证(离线可做的部分)

- `gofmt -l` 无输出;`go vet ./cmd/opensandbox-verify/` 通过;
  `go build ./cmd/opensandbox-verify/` 通过(go1.24.3 PATH + GOWORK=off)。
- `-h` 正常打印 flag 列表。
- 未设 `COCOLA_OPENSANDBOX_URL` 时秒退并提示 export(最常见误配)。

## 边界与遗留

- 真 server 端到端结果仍待合规环境产出(#22 未关闭)。
- 已知坑(沿用 P2 结论):docker bridge 模式下 `endpoints/44772` 可能返回
  容器内地址,host 不可达;若 Exec 卡在 connect,需给 resolveExecd 的
  endpoint 请求加 `?use_server_proxy=true`。harness 跑出来若卡 Exec,即此因。
- 未来真 server 跑通后:回填 Pause→Resume 延迟数据,并决定是否补
  WriteFile/ReadFile(目前用 exec 往返代偿)。
