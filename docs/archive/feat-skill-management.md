# feat: skill management

- 变更时间：2026-07-06 21:11 (+08:00)

## 变更理由

需要实现管理员和用户两侧的 Skill 管理能力：管理员可以导入共享 skill，用户可以选择启用共享 skill 并导入自己的 skill。运行时需要在 sandbox 启动 agent 前加载用户有效 skill，避免仅在提示词中描述能力而无法让 Claude Code 原生发现 `SKILL.md`。

## 变更内容

- apps/admin-api：扩展 `skill_entries` 数据模型，新增 skill zip 扫描/导入、用户偏好开关、用户有效 skill 列表和 bundle 下载能力。
- apps/admin-api/internal/objstore：复用 MinIO/S3 作为标准化 skill bundle 的对象存储，配置沿用 `COCOLA_MINIO_*`。
- db/migrations/00021_skill_packages.sql：为 skill 包元数据、manifest、frontmatter、用户偏好表补充迁移。
- apps/agent-runtime：按用户拉取有效 skill，并在 sandbox 内优先链接 `/data/plugins/skills/<id>` 共享卷；共享卷不存在时从 MinIO 下载 zip 并安全解压到 `$CLAUDE_CONFIG_DIR/skills/<id>`。
- apps/web：新增管理员 Skill 管理页、用户 Skill 管理页和详情页，支持上传 zip、从 Git 仓库扫描导入、全选导入、开关和移除个人 skill。
- apps/web/components/assistant-ui/workspace-shell.tsx：把用户侧 chat/skills 统一到共享 shell，避免侧边栏 tab 切换时重建 runtime 和 sidebar。
- deploy/docker-compose/docker-compose.full.yml：给 admin-api 补齐 MinIO 依赖和环境变量，确保正式 Docker 模式能保存 skill bundle。

## 关键取舍

- Git 仓库导入采用临时目录 shallow clone，默认扫描仓库 `skills` 目录；扫描和导入完成后清理临时仓库，导入前仍复用 zip skill 校验链路。
- 共享 admin skill volume 采用运行时优先 symlink 的兼容方案；后续只要有 publisher 把 admin skill 发布到 `/data/plugins/skills/<id>`，sandbox 同步会自动走零拷贝路径。
