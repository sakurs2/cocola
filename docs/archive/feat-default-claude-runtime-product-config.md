# feat: 默认使用 Claude Code 并配置化 Runtime 入口

- 变更时间：2026-07-24 00:14 (+08:00)

## 变更理由

多 Agent Runtime 能力仍需保留用于实验和未来扩展，但非 Claude Code Runtime
尚未达到生产兼容标准。面向普通用户继续展示 Runtime 选择会增加决策成本，并让
对话、Project 和模型协议出现不必要的组合差异。

## 变更内容

- `apps/gateway`：新增 Gateway 启动期产品配置、严格校验和认证读取接口；未显式指定
  Runtime 的新对话、定时任务和 Project 统一使用配置默认值。
- `apps/web`：加载产品配置并在 Picker 关闭时固定使用默认 Runtime；对话框、Project
  新建和编辑入口统一隐藏 Runtime Selector，配置异常时 fail closed。
- `apps/cli`、`.env.example`、`scripts/run-stack.sh`：补齐默认 Runtime 和 Picker
  配置，加入非 Claude Runtime 仍为实验状态的英文生产警告。
- `docs/configuration.md`：记录配置 owner、默认值、生效方式、失败策略和显式 API
  仍支持多 Runtime 的边界。
- 关键取舍：不删除 Codex Adapter 或 `runtime_id` 协议，不迁移或自动清理测试历史数据；
  Picker 可通过启动配置重新开启，但生产默认保持关闭。
