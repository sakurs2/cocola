# Changelog: hybrid 模式 llm-gateway 绑 0.0.0.0(修复"对话无响应")

日期:2026-07-02

## 症状

`make up-hybrid` 启动后与 AI 对话迟迟无响应。沙箱能正常创建、claude CLI
能跑到 `system init`,随后约 30s 静默,客户端断开,execd 报
`CommandExecError: exit status 1`。

## 根因

`--hybrid` 下沙箱"大脑"运行在**容器**内,经 `host.docker.internal:$LLM_PORT`
(Docker 宿主网桥)访问**原生** llm-gateway(run-stack.sh:376 注释已明示)。
但 `LLM_HOST` 默认 `127.0.0.1`,uvicorn 因此只绑 loopback
(日志坐实 `Uvicorn running on http://127.0.0.1:8081`)。

绑 loopback 的进程无法经网桥从容器内访问 —— 沙箱内 `curl host.docker.internal:8081`
直接 `Couldn't connect`。于是 claude 的首个模型请求连不通,干等到超时被断开,
表现为"对话无响应"。（sandbox-manager 绑 `:50051` 全网卡、gateway/agent-runtime
均为宿主本地互访,故建箱这一段能成功,唯独容器→宿主的 LLM 这一跳挂在 loopback。）

## 变更

`scripts/run-stack.sh`

- `LLM_HOST`:hybrid 模式下默认改绑 `0.0.0.0`(经 `_llm_host_default` 分支),
  非 hybrid 仍为 `127.0.0.1`;两者均可被 `COCOLA_LLM_HOST` 覆盖。
- `wait_port`:`nc -z 0.0.0.0` 不可靠,hybrid 下改探 `127.0.0.1`
  (0.0.0.0 绑定同样在 loopback 应答)。

## 验证

- `bash -n scripts/run-stack.sh` 语法通过。
- 端到端(用户侧复验):`make up-hybrid` 后发起对话,预期正常流式返回;
  可在沙箱内 `curl http://host.docker.internal:8081/` 得到应答(非 000)佐证。

## 相关

- 关联本次 opensandbox 卷属主修正
  (`docs/archive/fix-opensandbox-volume-ownership-chown.md`):二者同属 hybrid
  端到端对话链路的修复 —— 前者解记忆/续聊写权限,本条解模型调用连通性。
