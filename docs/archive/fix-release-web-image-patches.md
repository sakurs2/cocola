# fix: 修复 Release Web 镜像依赖安装失败

- 变更时间：2026-08-02 00:23 (+08:00)

## 变更理由

`v0.1.0` Release workflow 的 Web 镜像任务在 `pnpm install --frozen-lockfile` 阶段失败。
根目录 `package.json` 声明了 pnpm patched dependency，但 Web Dockerfile 在安装依赖前没有
把 `patches/` 复制进构建上下文中的工作目录，导致 pnpm 找不到
`@assistant-ui__react@0.14.24.patch`。

## 变更内容

- `apps/web/Dockerfile`：在执行 frozen install 前复制根目录 `patches/`。
- `scripts/tests/test_release_workflow.py`：增加顺序回归测试，防止依赖安装再次早于 patch
  文件复制。
