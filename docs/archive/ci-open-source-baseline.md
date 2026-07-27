# ci: 建立 1.0 开源协作与持续集成基线

- 变更时间：2026-07-27 15:04 (+08:00)

## 变更理由

cocola 即将进入 1.0 阶段，但原有 GitHub Actions 监听了错误的默认分支，
关键工作流从未在 `master` 上运行；仓库也缺少依赖更新、问题反馈、安全报告和
贡献指引等开源协作基线。此次变更以“少而有效”为原则补齐这些能力，不引入
自动回复机器人、复杂发布编排或尚未验证的分支保护。

全量校验还发现两个存量问题：`Part` 中重复的 `errorCode` JSON 标签会导致
错误码被静默丢弃；一个 scheduled task 测试夹具不再满足当前用户资料校验。

## 变更内容

- `.github/workflows/ci.yml`：改为监听 `master`，覆盖 Proto、Go workspace、
  sandbox-manager、Python、release 脚本和 Web；Action 固定到完整提交 SHA；
  Go lint 使用增量门禁，避免存量告警阻塞第一版 CI。
- `.github/workflows/release.yml`、`.github/workflows/sandbox-runtime-image.yml`：
  收紧权限、固定 Action SHA，并让发布在 CI 与 tag 校验通过后执行。
- `.github/workflows/proto-breaking.yml`：删除重复工作流，将 breaking check
  合并到 CI 的 Proto job。
- `.github/dependabot.yml`：按月检查 GitHub Actions、pnpm、Go 和 uv 依赖，
  合并 minor/patch 更新并限制并发 PR 数量。
- `.github/ISSUE_TEMPLATE/`、`.github/pull_request_template.md`、
  `CONTRIBUTING.md`、`SECURITY.md`：补充精简的协作与安全报告入口。
- `LICENSE`：替换为完整的 Apache License 2.0 标准文本。
- `packages/proto/buf.yaml`：保留 STANDARD 规则，仅豁免现有流式 RPC 的响应
  命名规则，避免为了启用 CI 制造 Proto breaking change。
- `apps/agent-runtime/pyproject.toml`、`apps/agent-runtime/uv.lock`：声明测试实际
  使用的 `jsonschema` 与固定版本 `claude-agent-sdk`，保证 locked 环境可运行。
- `apps/gateway/internal/convo/`：合并重复 JSON 标签为共享 `ErrorCode` 字段，
  并新增 memory-recall/run-summary 序列化回归测试。
- `apps/admin-api/internal/service/scheduled_tasks_test.go`：补齐用户名称测试夹具，
  使其符合当前账号资料契约。

关键取舍：暂不启用默认分支规则集；应在本变更推送且 CI 首次全绿后，再要求
PR 和必需检查，避免在无可用检查时锁住 `master`。
