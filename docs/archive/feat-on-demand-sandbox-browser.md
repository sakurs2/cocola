# feat: 增加按需 headless Browser 能力

- 变更时间：2026-07-19 22:14 (+08:00)

## 变更理由

Sandbox Runtime 已预装 Playwright 与 Chromium，但 Agent 只能临时编写脚本使用，缺少
统一的启停策略、持久状态目录、输出目录和机器可读协议。为后续 Skill、Artifact 与可选
MCP 复用同一能力，需要先在不引入可视化桌面或常驻控制端口的前提下建立 Browser 契约。

## 变更内容

- `deploy/sandbox-runtime/runtime-manifest.json`、`runtime-entrypoint.sh`：声明并准备
  `browser` capability；`coding` 默认启用、`minimal` 默认关闭，状态持久化到
  `/session/runtime/browser/profile`，输出默认进入 `/workspace/outputs/browser`。
- `deploy/sandbox-runtime/browser-runner.js`、`cocola_sandbox.py`：新增一次性 headless
  Playwright persistent context 与 `browser status/inspect/screenshot/pdf` 命令；仅允许
  HTTP(S)，输出路径限制在 Workspace，命令完成后关闭 Chromium，不开放控制端口。
- `deploy/sandbox-runtime/skills/cocola-sandbox-browser`：新增与 guest CLI 版本对齐的
  内置 Agent Skill，说明触发场景、命令流程、输出约定和安全边界；作为 root-owned
  Runtime 资产随镜像发布。
- `apps/agent-runtime/cocola_agent_runtime/skill_reconciler.py`、`server.py`：从当前镜像
  自省 platform Skill 清单，与 Admin/Personal Skill 原子合并并同时暴露给 Claude、
  Codex；平台 ID 禁止被市场 Skill 覆盖，镜像 Skill digest 变化会触发快照重建。
- `apps/sandbox-manager/internal/provider/opensandbox`：校验运维级
  `COCOLA_BROWSER_ENABLED`，删除 Agent 请求里的同名值并注入平台配置。
- `.env.example`、CLI compose、Runtime README、配置文档与 ADR-0025：同步 Profile、
  能力边界和运维覆盖方式。
- `apps/agent-runtime/tests`、`scripts/sandbox-runtime-verify.sh`：覆盖 Profile 优先级、
  Browser CLI、路径约束、平台/市场 Skill 合并与冲突、Claude/Codex 共享目录，以及
  真实容器内内置 Skill、页面解析、截图与 PDF。

本期仍不包含 Jupyter、可视化桌面、单 Sandbox observe、HTML 发布或 Sandbox MCP。
