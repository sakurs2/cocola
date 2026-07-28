# fix: Agent 能力编辑器反馈与卡片一致性

- 变更时间：2026-07-28 16:56 (+08:00)

## 变更理由

Agent Knowledge 前端和 Gateway 只允许 `feishu.cn`、`larksuite.com`，导致有效的
`*.larkoffice.com` 飞书 Wiki 链接在点击 Add 时被误判为不支持。Skills 与 Knowledge
又共用一个消息状态并统一渲染在能力编辑器末尾，使 Knowledge 校验错误看起来属于
Suggested Prompts。Agent Skills 的临时复选列表也没有复用 Skills 页面已有的视觉语言，
信息密度和页面一致性不足。Knowledge 的主动访问检查还需要独立代理接口、飞书凭证、
四类开放平台请求、并发控制和错误码状态映射，但实际运行时仍会由 `lark-cli` 返回权威
权限结果，不符合当前阶段保持产品简单的目标。

## 变更内容

- `apps/gateway/internal/agentprofile/knowledge.go`：将 `larkoffice.com` 加入受控的
  飞书/Lark Knowledge 域名白名单；仍只调用固定开放平台域名，不直接请求用户输入 URL。
- `apps/gateway/internal/agentprofile/service_test.go`：覆盖 Lark Office Wiki 链接的类型推断、
  查询参数清理和规范化。
- `apps/web/components/agents/agent-capabilities-editor.tsx`：拆分 Skills 与 Knowledge
  反馈状态，把 URL 错误收回 Knowledge 输入区；Add 增加按压和结果反馈；Skills 使用与
  Skills 页面一致的图标、摘要、来源标签和三列卡片布局，但保留整卡选择交互且不显示
  Enable 操作。
- `apps/web/app/agents/[id]/page.tsx`、`apps/web/app/api/agents/[id]/knowledge/check/`：
  移除页面加载、保存和手动触发的 Knowledge 主动访问检查，以及对应 Web 代理路由。
- `apps/gateway/internal/httpapi/agent_knowledge.go`、`agent_knowledge_test.go`、
  `api.go`：移除 Knowledge 检查 handler、飞书探测客户端、状态映射、路由及专用测试；
  Knowledge 保存、会话快照和运行时按需读取保持不变。
- `apps/web/lib/agent-capabilities.test.mjs`：覆盖 Lark Office 域名、Knowledge 内联反馈、
  Add 动效、Agent Skill 卡片关键结构，并防止 Check access 链路重新进入页面。
