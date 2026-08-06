# refactor: own the Web UI compatibility layer

- 变更时间：2026-08-06 13:46 (+08:00)

## 变更理由

Cocola Web 此前直接依赖授权的 HeroUI Pro 组件与样式，导致本地安装、CI 和自部署都需要额外的授权令牌，也使页面行为受外部私有组件版本约束。迁移目标是在保持现有页面视觉与交互的前提下，由 Cocola 自己维护实际使用的组件契约，后续仅把 HeroUI Pro 当作设计参考。

## 变更内容

- `packages/ui-compat/`：新增 Cocola 自有兼容组件层，覆盖布局、导航、列表、数据表格、抽屉、卡片、聊天消息和输入框等生产页面实际使用的组件。
- `apps/web/`：将用户端与 Admin 的组件导入批量切换到 `@cocola/ui-compat`，保留 Cocola 的业务状态、数据请求和交互逻辑。
- `apps/web/app/globals.css`：移除授权组件样式入口，改为加载 Cocola 自有兼容样式。
- `apps/web/package.json`、`pnpm-lock.yaml`：移除生产 HeroUI Pro 依赖及其安装链路，使常规安装和构建不再需要授权令牌。
- `CONTRIBUTING.md`：更新开发环境说明，明确 HeroUI Pro 只作为视觉参考，不得成为运行、CI 或部署依赖。
- 删除迁移期间使用的 Pro/Compat 对照实验室、实验室脚本和仅供实验室使用的 barrel 导出，避免废弃代码继续携带授权依赖。
- 更新 UI 结构测试，锁定 Cocola 兼容层导入和无 Pro 生产依赖约束。

关键取舍：兼容层只实现 Cocola 当前真实使用的公开契约，不复制未使用的 Pro 能力；生产代码继续依赖 HeroUI OSS 的基础组件和 token。
