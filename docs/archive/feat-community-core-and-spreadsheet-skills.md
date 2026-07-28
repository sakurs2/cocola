# feat: 增加 Community 默认 Skill 与 Sandbox 表格能力

- 变更时间：2026-07-28 11:55 (+08:00)

## 变更理由

Cocola 需要在不开放动态 Skill 搜索和安装的前提下，提供一组克制、可审计的默认能力。
平台自建且依赖 Sandbox Runtime 的表格处理能力应随镜像发布；第三方开源 Skill 则应进入
Admin 默认 Catalog，由管理员保留禁用和人工接管权。

## 变更内容

- `deploy/sandbox-runtime/skills/cocola-spreadsheet/`、`manifest.json`：增加随镜像发布的
  Platform Skill，使用 Runtime 固定的 `openpyxl` 处理本地 CSV/XLSX，通过
  `/workspace/outputs` 交付 Artifact，并明确公式不计算、输入不覆盖和不执行宏等边界。
- `apps/admin-api/internal/defaultskills/assets/`：增加只包含 `frontend-design` 的
  `community-core` Bundle，固定到 `anthropics/skills` 的完整 commit SHA，保留 Apache
  2.0 LICENSE、确定性 ZIP 和 SHA-256。
- `apps/admin-api/internal/defaultskills/defaultskills.go`、`cmd/admin-api/main.go`：启动时
  依次加载并对账 lark-cli 与 community-core 两个默认集合，复用现有 bundled Skill
  幂等、禁用状态保留和管理员人工接管语义。
- `scripts/update-community-core-skills.sh`：增加显式升级脚本，只接受完整上游 commit，
  并用固定时间戳生成可复现的 Bundle。
- Admin API 与 Agent Runtime 测试、Sandbox Shim selfcheck、Runtime 验证脚本：覆盖上游
  摘要、许可证、Catalog 来源字段、镜像 Skill manifest、固定 Python/openpyxl 契约和
  root-owned 资产。
- `.env.example`、`docs/configuration.md`、`deploy/sandbox-runtime/README.md`：说明默认
  集合和 Platform Spreadsheet Skill 的发布、依赖与管理边界。

## 关键取舍

- 不引入 `find-skills`、`npx skills`、Skill 市场搜索、动态安装或新的出网白名单。
- `cocola-spreadsheet` 不复制 Anthropic 的 source-available `xlsx` Skill，使用 Cocola
  自有指令和镜像中已有依赖完成第一版能力。
