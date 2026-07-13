# ADR-0020: 配置单一所有权与热加载边界

- Status: Accepted
- Date: 2026-07-12
- Deciders: @cocola-maintainers

## Context

MVP 演进中出现了 env、JSON、Admin DB、Redis override 和脚本变量表达同一配置的
情况。典型后果是 Web 模型列表来自 DB，而 LLM Gateway 可能读取另一份 JSON；Warm
Pool 在 Admin 中保存成功，但 Redis 发布失败时实际配置不变。系统表现为“配置成功”
却无法判断哪个值生效。

## Decision

1. 模型、Prompt、MCP、Skill 和少量运行参数由 Admin/Postgres 唯一持有并热加载。
2. 地址、Secret、基础设施、沙箱 provisioning、资源和超时是进程启动配置，修改后
   重启对应进程。
3. Redis 不作为配置真源，只承担缓存、租约、事件和权限传播；Warm Pool sizing 通过
   Redis 投递，但以 Postgres system setting 为准并周期对账；投递值不可用时暂停
   本轮扩缩容，不回退到可能过期的启动默认值。
4. Warm Pool 默认开启；Enabled/Size 热加载，Image/模型路由/refill interval 是启动配置。
5. OpenSandbox 是唯一生产沙箱后端；Provider 接口仅保留为内部边界和测试 seam。
6. Fake LLM provider 仅用于 hermetic tests，生产模型缺失时明确报错，不隐式回退。
7. 删除配置别名和永久灰度开关；一个含义只保留一个名称。

## Consequences

- 管理页只展示真正可热更新的配置，不再伪装成跨进程环境变量浏览器。
- 配置变更的生效方式可预测；启动配置换取一次明确重启，避免分裂中间态。
- 首次部署需要在 Admin 中配置模型，不再通过 `.env` 偷偷创建 Fake/真实路由。
- 未来若需要新的热加载项，必须先明确 owner 和持久化语义，不能增加 Redis shadow
  config。
