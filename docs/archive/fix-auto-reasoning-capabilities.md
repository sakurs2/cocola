# fix: 推理能力改为按模型协议自动派生

- 变更时间：2026-08-07 13:00 (+08:00)

## 变更理由

推理等级属于用户在对话框中的运行时选择，管理员应只负责配置 Provider 和 Model。此前 Admin 模型表单额外要求管理员声明原生推理档位，导致普通模型配置默认只开放 Auto，也把内部协议能力错误地暴露成了管理决策。

## 变更内容

- `apps/web/app/admin/models/page.tsx`：移除 Admin 模型表单中的 Reasoning effort 控件及请求字段。
- `apps/admin-api/internal/httpapi/handlers.go`：Admin 模型 API 不再接收推理档位配置。
- `apps/admin-api/internal/service/admin.go`：根据模型协议自动维护原生能力；Anthropic Messages 与 OpenAI Responses 分别使用各自支持的 effort 集合，Embedding 不提供推理能力。
- `db/migrations/00058_default_reasoning_efforts.sql`：为已经存在的模型路由回填协议默认能力，兼容已经执行过 00057 的环境。
- `apps/admin-api/internal/service/llm_models_test.go`：验证模型创建和编辑时均自动恢复协议能力。
- 关键取舍：管理员只配置模型，用户只在对话框选择 Auto / Fast / Deep / Max；路由能力仍作为后端内部元数据用于前端可用性和网关校验。
