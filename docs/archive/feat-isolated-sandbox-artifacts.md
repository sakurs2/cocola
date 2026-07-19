# feat: 建立隔离的 Sandbox Artifact 与 HTML 预览契约

- 变更时间：2026-07-20 00:51 (+08:00)

## 变更理由

Cocola 已有 `/workspace/outputs` 自动上传和右侧 Artifact 面板，但这项行为没有进入版本化
Sandbox Runtime contract，Agent 只能依赖简短 system prompt 猜测发布约定；输出扫描还会
跟随符号链接。HTML 虽通过 `srcdoc` iframe 展示，Artifact 下载接口仍会把用户生成内容以
inline 形式放在 Cocola 鉴权同源，iframe 也允许脚本、表单、弹窗和外部资源，安全边界过宽。

## 变更内容

- `deploy/sandbox-runtime/runtime-manifest.json`、`cocola_sandbox.py`：新增两个 Profile
  都具备的 Artifact capability，以及有上限、仅枚举普通文件的 `artifact status/list`。
- `deploy/sandbox-runtime/skills/cocola-sandbox-artifacts`：新增镜像内置 Skill，指导 Agent
  使用 `/workspace/outputs`、检查最终清单，并生成单文件自包含 HTML。
- `apps/agent-runtime/cocola_agent_runtime/server.py`：Artifact 快照不再跟随目录或文件
  符号链接，只发布发生变化的普通文件；system prompt 与 guest CLI/HTML 边界对齐。
- `apps/gateway/internal/httpapi/api.go`、Web Artifact API：下载强制 attachment、nosniff、
  no-store、同源资源策略和 deny-by-default CSP，并完整转发这些响应头。
- `apps/web/components/assistant-ui/file-preview.tsx`：图片/PDF 改为 fetch 后使用 object URL；
  HTML 在 2 MiB 上限内惰性解析和移除主动内容/外部 URL，再在无权限 opaque-origin iframe
  中静态渲染。
- Runtime/Python/Go 测试、`scripts/sandbox-runtime-verify.sh`、ADR-0026 与配置/运行时文档：
  覆盖能力清单、Skill、符号链接过滤、下载响应头和真实镜像链路。

本期不增加常驻服务、自动打开网站预览 Tab、Jupyter、单 Sandbox observe 或 Sandbox MCP。
