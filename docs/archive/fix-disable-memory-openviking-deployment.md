# fix: 暂停 Memory 能力并移除 OpenViking 部署依赖

- 变更时间：2026-08-02 12:10 (+08:00)

## 变更理由

当前 Memory 能力仍在开发中，但生产与本地开发部署会默认启动 OpenViking，并在尚未配置模型的新环境中持续触发不可用的记忆请求。这增加了部署体积、启动故障面和无意义的错误日志，也让管理员误以为该能力已经可以启用。

## 变更内容

- `apps/cli/internal/assets/compose.yaml`、`deploy/docker-compose/docker-compose.dev.yml`、`scripts/run-stack.sh`：移除 OpenViking 服务、数据卷、端口等待和相关环境变量，不再把它作为生产或开发部署依赖。
- `apps/cli/internal/config/`：安装与升级配置不再生成 OpenViking 和 Memory 内部服务配置，并增加回归断言。
- `apps/gateway/`：启动时不再构造 Memory 服务；Memory 设置接口固定返回关闭状态。
- `apps/admin-api/`：Memory 配置固定为开发中且不可启用，更新请求返回功能不可用；Embedding Model 的管理与连接测试保持不变。
- `apps/web/`：Admin Toolbox 与用户资料页统一展示 Memory 正在开发中的静态提示，不再发起 Memory 数据请求。
- `apps/llm-gateway/`：移除 Memory 内部适配端点的部署令牌要求，使该休眠链路不再成为服务启动前置条件。

保留现有 Memory 数据表与底层实现代码，避免未来恢复开发时进行破坏性迁移；当前所有入口均由代码固定关闭，不新增用户配置项。
