# perf: 批量同步会话 Skill

- 变更时间：2026-07-11 01:36 (+08:00)

## 变更理由

新会话启动时，agent-runtime 会把用户启用的 Skill 同步到 sandbox。原实现逐个 Skill
执行检查、对象下载、文件写入和解压；当用户启用 17 个 Skill 时，大量串行 sandbox
调用会让 `Environment ready` 和 Agent 首次推理延迟十余秒甚至更久。

## 变更内容

- `apps/agent-runtime/cocola_agent_runtime/server.py`：并发读取 Skill bundle，组装单个批量运输包，对 sandbox 只执行一次 `write_bytes` 和一次安装命令。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：批量安装继续优先使用镜像内 shared Skill；本地 bundle 在 staging 中全部完成路径校验后才替换现有目录。
- `apps/agent-runtime/tests/test_server.py`：覆盖批量调用次数、bundle/Markdown/shared 混合安装，以及恶意 ZIP 不覆盖现有 Skill。
- 不修改 Agent/Sandbox Proto、Skill 数据结构、管理接口和 system prompt 行为。
