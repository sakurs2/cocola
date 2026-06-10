# fix: 隔离 Claude CLI 配置目录，修复 503

- 变更时间：2026-06-10 13:00 (+08:00)
- 关联提交：f334264

## 变更理由
Web 端发起请求后偶发 503，且 llm-gateway 收不到 `POST /v1/messages`。
根因：全局 `~/.claude/settings.json` 的 `env` 块被 Claude Code 以高于
SDK 注入进程环境变量的优先级应用，覆盖了我们注入的 `ANTHROPIC_BASE_URL`，
导致请求绕过 llm-gateway 直接打到公网端点。实测 `--setting-sources=` 与
`--settings` 均无法覆盖，只有让 CLI 指向独立配置目录才生效。

## 变更内容
- apps/agent-runtime/cocola_agent_runtime/claude_sdk_provider.py：
  - 每个 provider 创建一个私有空配置目录，写入 `{}` 的 settings.json，
    通过 `CLAUDE_CONFIG_DIR` / `ANTHROPIC_CONFIG_DIR` 注入；
  - 由于空配置丢失了用户原有的禁用开关，重新置位
    `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` 抑制遥测流量；
  - 通过 atexit 在退出时清理临时目录。
- apps/llm-gateway/cocola_llm_gateway/server.py / service.py：
  上游 drain 失败与 stream error 分支补充 warn 日志，便于暴露根因。
- 验证：单测全过，端到端返回 pong，`POST /v1/messages 200`。
