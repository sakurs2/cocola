# feat: run-stack 三阶段优雅退出 + 端口预检

- 变更时间：2026-06-10 13:20 (+08:00)
- 关联提交：e889fbf

## 变更理由
用户诉求：退出本地开发栈后不能残留端口占用。macOS 上 `go run` / `uv run`
/ `pnpm dev` 会 fork 出真正监听端口的孙进程，并被 reparent 到 launchd，
脱离进程组，导致 `kill -- -$pid` 无法触及，端口残留。

## 变更内容
- scripts/run-stack.sh：
  - 每个服务启动前调用 `free_port` 预检，避免被残留监听者占用而启动失败；
  - 三阶段 `cleanup()`：先对各进程组 SIGTERM，等约 3s，再 SIGKILL 幸存者，
    最后按"已占用端口"逐个 `free_port` 兜底释放；
  - `_SHUTTING_DOWN` 守卫，避免退出过程中重复 Ctrl-C 触发重入。
  - 结果：退出后四个端口（50061 / 8080 / 8081 / 3000）全部释放。
