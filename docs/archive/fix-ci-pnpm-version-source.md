# fix: 修复 CI 中重复的 pnpm 版本来源

- 变更时间：2026-07-27 15:28 (+08:00)
- 关联运行：https://github.com/sakurs2/cocola/actions/runs/30246064697

## 变更理由

首次运行新的 CI 时，Web job 在 `pnpm/action-setup` 阶段失败。根因是 workflow
通过 `version: 9` 指定了一次版本，而根 `package.json` 又通过
`packageManager: pnpm@9.0.0` 指定了一次；新版 Action 会拒绝同时存在两个
版本来源，后续安装、测试和构建均未执行。

## 变更内容

- `.github/workflows/ci.yml`：删除 Action 中重复的 `version` 参数，统一以根
  `package.json` 的 `packageManager` 字段为唯一 pnpm 版本来源。

关键取舍：不修改项目声明的 pnpm 版本，也不放宽 Web 校验范围。
