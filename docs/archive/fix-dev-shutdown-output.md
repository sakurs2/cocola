# fix: 精简开发栈退出输出

- 变更时间：2026-07-14 11:50 (+08:00)

## 变更理由

macOS 缺少 `setsid` 时，开发脚本用 Bash job control 为各服务建立独立进程组。Ctrl+C 优雅停机期间，Bash 会把每个后台任务的终止通知输出到终端，形成大量无操作价值的日志，掩盖真正的停机结果。

## 变更内容

- `scripts/run-stack.sh`：所有服务启动并取得独立进程组后关闭 monitor mode，保留原有分组信号、最长 30 秒等待、强制清理和端口回收逻辑，同时抑制逐任务终止通知；真正执行清理的 `cleanup()` 固定输出停止服务并等待 checkpoint、清理残留进程、释放端口三个步骤及总耗时。
- `scripts/run-stack-dev.sh`：等待应用栈前关闭 monitor mode；外层 supervisor 不再承担停机提示，避免 Ctrl+C 直接到达内层应用栈时完全没有输出，也避免双方同时收到信号时重复打印。
- checkpoint、Sandbox Manager 优雅退出和 OpenSandbox port-forward 停止顺序保持不变。
