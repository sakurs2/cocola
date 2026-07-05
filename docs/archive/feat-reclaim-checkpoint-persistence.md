# feat: 回收前 checkpoint 持久化

- 变更时间：2026-07-05 03:03 (+08:00)

## 变更理由

多节点调度下继续使用 local-path 作为 sandbox 运行时 workspace 时，sandbox 被调度到其他节点后无法恢复关键会话文件。相比引入 NFS，本次采用更轻量的回收前 checkpoint：只在 cocola 控制面可感知的 sandbox 回收路径前，对关键目录打包并上传对象存储，fresh sandbox 创建后再按 latest checkpoint 恢复。

## 变更内容

- apps/agent-runtime/cocola_agent_runtime/checkpoint.py：新增 checkpoint manager，默认持久化 `/home/cocola/.claude`、`/workspace/uploads`、`/workspace/outputs`、`/workspace/persist`，支持配置化目录、超时、大小上限、对象 key 生成、回收前上传和 fresh sandbox restore。
- apps/agent-runtime/cocola_agent_runtime/session_map.py：扩展 session_map，记录 latest checkpoint object key，Postgres/Memory 实现保持一致。
- apps/agent-runtime/cocola_agent_runtime/server.py：ReleaseSession 在释放 sandbox 前尽力 checkpoint；fresh sandbox acquire 后、agent 执行前尽力 restore。
- apps/agent-runtime/cocola_agent_runtime/__main__.py：在 agent-runtime composition root 中接入 checkpoint manager。
- apps/sandbox-manager/internal/provider/provider.go、apps/sandbox-manager/internal/orchestrator/*：新增可选 SessionCheckpointer hook，并在 idle reaper pause 与显式 Release 前调用；失败不阻塞回收。
- apps/sandbox-manager/internal/provider/checkpoint、apps/sandbox-manager/cmd/sandbox-manager/main.go：当 MinIO、Postgres 配置齐全时默认包装真实 provider，在 sandbox-manager 内执行回收前打包、上传对象存储并写入 session_map metadata。
- deploy/docker-compose/docker-compose.full.yml：给 sandbox-manager/agent-runtime 补充 checkpoint 目录、超时、大小上限环境变量模板；checkpoint 不再需要启用开关。
- apps/agent-runtime/tests、apps/sandbox-manager/internal/orchestrator/binder_test.go：补充 checkpoint/restore 行为测试。

关键取舍：v1 不做每轮 checkpoint，也不承诺节点宕机、OOMKill 等非受控故障的临终 snapshot；这类场景只能恢复上一次成功 checkpoint。
