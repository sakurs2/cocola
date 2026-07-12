# feat: 单 Gateway 长任务对话可靠性

- 变更时间：2026-07-12 11:21 (+08:00)

## 变更理由

MVP 的 Agent 执行生命周期绑定浏览器 SSE 请求，刷新、断网或代理断开会终止长任务；
Gateway/Agent Runtime 重启可能留下永久 `running`，默认 Exec、模型和 Token 时限也无法
稳定覆盖数十分钟回答。项目不要求高可用或跨进程接管，因此采用单 Gateway 后台 Run +
最新快照模型，避免事件日志、租约和工具重放协议。

## 变更内容

- `apps/gateway`：新增最小 Run Store、owner 校验、请求幂等、同会话单飞、后台执行、
  1 秒草稿、snapshot 重连、显式取消、SSE ping、正确终态与 30 秒优雅停机。
- `apps/agent-runtime`：Run/Exec 统一 3600 秒上限，20 秒 sandbox heartbeat，gRPC
  Exec 可取消，SessionMap/checkpoint 按用户隔离，real 模式缺 executor 时 fail closed。
- `apps/sandbox-manager`：复用 binding 前校验 session owner；Warm Pool 保持默认开启，
  claim 后仍走 checkpoint restore 并接受 active heartbeat。
- `apps/llm-gateway`：单次模型调用默认超时调整为 300 秒。
- `deploy/docker-compose`：删除 sandbox-manager 重复的 `minio-init` YAML key，并默认
  传入长任务超时与 heartbeat 配置。
- `apps/web`：保存 `run_id`，刷新/断线后按完整 snapshot 恢复；Stop/删除显式调用
  cancel API，普通导航和订阅断开不再取消 Agent。
- `db/migrations`：新增 `client_request_id` 和 running 单飞约束，并收敛曾短暂落地的
  分布式 Run 字段/事件表，回填或清理无 owner 的历史 SessionMap 索引。
- 关键取舍：不实现多 Gateway、事件逐条持久化、worker lease 或跨进程执行接管；进程
  故障统一标记 `interrupted` 并保留最新 partial answer。
