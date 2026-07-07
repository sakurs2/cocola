# feat: admin architecture DAG

- 变更时间：2026-07-07 10:54 (+0800)

## 变更理由

管理员需要一个直观页面查看 cocola 当前系统架构和组件健康状态，用 DAG 表达 Web、Gateway、Agent Runtime、Sandbox、OpenSandbox 与基础依赖之间的关系。页面只承担架构拓扑和 health 摘要职责，不替代 Redis、MinIO、Postgres 的专业看板。

## 变更内容

- apps/admin-api：新增 `/admin/architecture` 接口，返回固定 DAG 拓扑、组件 health 状态和关键摘要；健康探测失败不会导致整个接口失败。
- apps/web：新增 `/admin/architecture` 页面和 BFF 代理，使用可拖动/缩放大画布、轴对称拓扑布局、加粗 SVG 单段流动曲线和节点卡片展示 DAG；悬浮或选中节点时高亮直连节点与边，不引入图形库。
- scripts/run-stack.sh、deploy/docker-compose/docker-compose.full.yml：为 admin-api 显式提供 LLM Gateway / Sandbox Manager 地址，便于架构页探测。
- 测试覆盖 DAG 返回、健康状态映射、未配置依赖 unknown，以及不暴露 latency / recent error 字段。
