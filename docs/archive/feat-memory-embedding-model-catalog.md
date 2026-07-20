# feat: 将 Embedding 模型收敛到模型目录

- 变更时间：2026-07-21 00:56 (+08:00)

## 变更理由

Embedding 模型需要继续由 Models 统一管理，但创建体验应只暴露模型名、Base URL 和 API Key，并让 Memory 及未来知识库通过模型路由复用。实际联调还发现三个稳定性问题：Memory 未启用时仍会短暂显示 Recall 节点；Recall miss 删除节点会导致 assistant-ui 的 PartByIndex 越界；Anthropic 上游虽返回 200，但 Memory Adapter 未读取 PASSTHROUGH 文本，导致 Capture 进入 EXTRACTION_FAILED 重试。

## 变更内容

- `apps/admin-api`：增加精简的 Embedding 模型创建、编辑和连通性测试接口，自动探测并保存向量维度，安全复用已有凭证并清理孤立的内部 Provider。
- `apps/web/app/admin/models`、`apps/web/app/api/admin/embedding-models`：在 Models 页面提供 Chat / Embedding 类型选择；Embedding 表单只保留模型名、Base URL、API Key 和连接测试，模型保持隐藏但可被平台能力选择。
- `apps/gateway/internal/memory`、`apps/gateway/internal/convo`：仅在全局和用户 Memory 均启用后发送 running；保留并隐藏 miss 占位，避免完成态渲染索引失效。
- `apps/llm-gateway`：Embedding 请求不强制厂商可选的 dimensions 参数，但继续校验返回维度；Memory 抽取同时解析普通 CONTENT_DELTA 和 Anthropic PASSTHROUGH text_delta。
- `apps/web/app/profile`、对话与分享页：补齐 Toast Provider，并保持 Memory miss 不可见且不生成过程摘要。
- 补充 Admin、Gateway、LLM Gateway 和 Web 回归测试，覆盖模型路由、连接探测、Recall 启停、miss 稳定性及 Anthropic 抽取事件。
