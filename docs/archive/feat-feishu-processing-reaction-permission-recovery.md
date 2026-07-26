# feat: 飞书处理状态表情与缺权引导

- 变更时间：2026-07-26 20:26 (+08:00)

## 变更理由

用户通过飞书机器人发起 Agent 对话后，长时间运行期间缺少明确的“正在处理”反馈。新创建和已有的飞书应用还可能缺少消息表情权限；该辅助能力不应阻塞 Agent 主回答，已有应用需要获得可点击的权限申请入口。

## 变更内容

- `apps/gateway/internal/channel/feishu/registration_sdk.go`：新创建的机器人增加 `im:message.reactions:write_only`，继续只申请当前私聊能力需要的最小权限集合。
- `apps/gateway/internal/channel/feishu/reaction.go`：通过官方 Go SDK 添加和删除 `Typing` 表情；结构化读取缺权响应，并只接受飞书/Lark 官方 HTTPS 权限链接。
- `apps/gateway/internal/channel/feishu/manager.go`：inbox worker 开始处理时添加表情，最终回复完成后清理；表情失败不影响 Agent 主链路。缺权调用退避 10 分钟，提示 24 小时最多一次，内存状态随 Runner 生命周期释放。
- `apps/gateway/cmd/gateway/main.go`：将现有 `COCOLA_PUBLIC_ORIGINS` 传给飞书 Manager，飞书错误未提供可信链接时回退到 Cocola Connector 页面。
- `apps/gateway/internal/channel/feishu/*_test.go`：覆盖 Reaction API、注册权限、可信链接校验、生命周期、缺权退避、提示限频和主回答不被阻塞。

关键取舍：

- 不新增数据库字段、定时任务或用户 OAuth；Gateway 重启后最多重复提示一次。
- 不读取用户或其他机器人的表情，因此不申请 Reaction 读取权限，也不订阅 Reaction 事件。
- 飞书侧仍可能要求应用发布和租户管理员审批，Cocola 只提供官方入口，不能绕过平台审核。
