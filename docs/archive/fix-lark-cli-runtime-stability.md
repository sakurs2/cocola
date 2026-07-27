# fix: 修复默认 Skill 与飞书运行时并发稳定性

- 变更时间：2026-07-27 20:03 (+08:00)

## 变更理由

- 默认 Skill 启动对账在读取记录和最终更新之间包含对象存储 I/O，管理员并发禁用或人工
  接管同 ID Skill 时，旧快照可能覆盖较新的管理员操作。
- TAT singleflight 只按 Connector ID 合并；Connector 重配与旧版本刷新并发时，新版本
  请求可能收到旧应用凭证，旧刷新也可能覆盖新版本缓存。
- Execute 对话在 Agent Stream 前同步解析可选飞书凭证，且使用无 deadline 的 Run
  context；数据库或鉴权端点故障会阻塞普通对话。
- 飞书长连接 ready 状态落库失败后仍启动附件 worker，数据库中的非 ready 状态会让附件
  连续重试后永久失败。

## 变更内容

- `apps/admin-api/internal/store/`、`internal/service/admin.go`、
  `internal/service/defaultskills.go`：
  - 增加原子 `SetSkillEnabled`，避免启停操作整行写回旧 Skill 内容。
  - 增加带 `source_type='bundled'` 条件的 `UpdateBundledSkill`，仅更新发布包拥有的内容
    字段并保留 `enabled`；人工接管后返回冲突并跳过对账覆盖。
- `apps/admin-api/internal/service/defaultskills_test.go`、`internal/store/parity_test.go`：
  覆盖对象上传窗口内的并发禁用、人工接管和 Memory/Postgres 存储契约。
- `apps/gateway/internal/channel/feishu/token.go`：singleflight key 加入 Connector
  Version、Domain 和 AppID；缓存拒绝较旧版本刷新覆盖较新版本。
- `apps/gateway/internal/httpapi/simple_chat.go`：可选飞书凭证解析限制为 5 秒，超时后将本轮
  飞书能力标记为暂不可用并继续普通对话。
- `apps/gateway/internal/channel/feishu/manager.go`：ready 状态写入使用 5 秒上限；只有落库
  成功才启动 worker，失败或租约丢失时取消 runner，由现有 reconcile 重建连接。
- 对应测试增加版本并发、超时降级和 ready 状态持久化失败场景；问题 5 的旧 Sandbox
  出口策略按用户要求保持不变。
