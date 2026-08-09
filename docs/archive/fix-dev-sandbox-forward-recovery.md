# fix: 开发环境 OpenSandbox 转发自动恢复

- 变更时间：2026-08-09 10:39 (+08:00)

## 变更理由

最新 Project 对话在 Sandbox Acquire 阶段失败。Agent Runtime 日志显示访问 `127.0.0.1:8090` 被拒绝，OpenSandbox 转发日志同时记录 `lost connection to pod`。根因是 `make dev` 只在启动时创建一次 kubectl port-forward；转发退出后主服务仍然在线，却没有进程负责重建转发。

## 变更内容

- `scripts/run-stack-dev.sh`：为 kubectl port-forward 增加受控监管循环，子转发退出后自动重启；关闭开发栈时仍按进程树停止监管进程和当前子进程；初次健康检查失败时主动清理残留监管进程。
- `scripts/run-stack-dev-test.sh`：增加转发监管能力的回归检查。
- 实机主动终止 kubectl 子进程后，监管进程在 1 秒内创建新子进程，OpenSandbox `/health` 恢复 200；Project Git Refresh 随后成功更新 Sandbox 内的 Git 快照。
