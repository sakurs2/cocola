# fix: 过滤低相关度长期记忆

- 变更时间：2026-07-25 17:15 (+08:00)

## 变更理由

长期记忆召回此前只限制 Top-K，没有设置最低相关度。只要 OpenViking 返回结果，
Gateway 就会将其注入 Agent 上下文，存在低相关记忆干扰当前回答的风险。

## 变更内容

- `apps/gateway/internal/memory/openviking.go`：固定使用 OpenViking 自动召回的默认
  阈值 `0.15`，在请求端下推 `score_threshold`，并在客户端对返回结果再次过滤。
- `apps/gateway/internal/memory/service_test.go`：覆盖请求阈值，以及 `0.149` 被过滤、
  `0.15` 边界值被保留的行为。
- 关键取舍：阈值作为代码常量而非管理配置，减少不必要的控制面复杂度；客户端兜底
  过滤用于保持不同 OpenViking 兼容实现下的一致行为。
