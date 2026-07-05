# feat: html preview and sandbox toolbelt

- 变更时间：2026-07-05 21:41 (+08:00)
- 关联提交：待提交

## 变更理由

HTML artifact 当前只能按源码文本预览，用户无法直接看到渲染效果。同时 cocola sandbox runtime 基于 OpenSandbox base，缺少浏览器、截图、文档/媒体处理和部分基础排障工具，导致 agent 在沙箱内做前端验证、页面截图和文件检查时需要临时安装依赖。

## 变更内容

- apps/web/app/page.tsx：为 `.html` / `text/html` artifact 增加 iframe 渲染预览，并提供预览/源码切换；下载能力保持不变。
- deploy/sandbox-runtime/Dockerfile：预装 `wget`、`fd`、`yq`、`tree`、`file`、`make`、`build-essential`、`pkg-config`、Playwright-managed Chromium、`pnpm`、`yarn`、Playwright、`poppler-utils`、ImageMagick、`librsvg2-bin` 和 Noto 字体等常用工具。
- deploy/sandbox-runtime/shim/agent_shim.py：扩展 `--selfcheck`，报告浏览器、前端、文档/媒体和基础命令是否存在。
- scripts/sandbox-runtime-verify.sh：把新增工具纳入 sandbox runtime 验证。
- deploy/sandbox-runtime/README.md：补充预装工具清单和 Playwright-managed Chromium 的示例。

## 关键取舍 / 注意事项

- 不在沙箱启动时安装依赖，所有工具都进入镜像构建阶段。
- v1 只预装 Chromium，不引入 Jupyter、VS Code server 或任何常驻监听端口。
- Ubuntu 24.04 的 apt `chromium` 是 snap 占位包，容器内不可用；因此构建期下载 Playwright-managed Chromium，并通过 `/usr/local/bin/chromium` 暴露给通用脚本。
