# fix: 对话删除保护与有界重试

- 变更时间：2026-07-12 12:48 (+08:00)

## 变更理由

运行中的对话在取消接口返回 202 后会被前端立即删除。后台 Run 随后写入最终 assistant
message 时可能因 conversation 外键已不存在而失败，原有 finalizer 又会无限重试，造成
Run 长期停留在 `running`，并持续占用 goroutine 和数据库连接。Web 的首次 POST 与 SSE
重连也没有明确的失败次数上限。

## 变更内容

- `apps/gateway/internal/httpapi/api.go`：删除 conversation 前与 Run 创建串行检查；存在
  active Run 时返回 409，不执行 release 或删除。
- `apps/gateway/internal/httpapi/simple_chat.go`：最终持久化限制为四次、每次三秒的正常
  尝试；失败后仅再做一次三秒、无 assistant message 的 `interrupted` 收尾，彻底数据库
  故障时停止后台重试并保持 readiness 失败，等待启动恢复处理残留状态。
- `apps/web/app/runtime-provider.tsx`：删除不再隐式取消；只有服务端确认删除成功后才清理
  本地订阅。首次 POST 限制八次，单次订阅重连限制二十次，cursor 存储失败不再中断运行。
- `apps/web/components/assistant-ui/app-sidebar.tsx`：删除弹窗明确提示需先 Stop 并等待完成，
  同时展示服务端冲突提示。
- `apps/gateway/internal/httpapi/simple_chat_test.go`：覆盖运行中删除冲突、终态后删除以及
  finalizer 次数上限和 `interrupted` fallback。

关键取舍：不引入删除队列、数据库任务或分布式协调。删除和 Run 创建复用现有串行边界，
以一个明确的 409 状态避免无法收尾的中间态。
