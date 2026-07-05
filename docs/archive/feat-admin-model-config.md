# feat: admin model configuration

- 变更时间：2026-07-05 13:54 (+08:00)

## 变更理由

管理员需要在 cocola admin 中配置模型、API key、base URL、logo 和默认模型，并让用户对话界面看到的模型列表来自管理员配置。原链路依赖 `COCOLA_LLM_CONFIG` JSON 文件，web 和 llm-gateway 各自读取静态配置，不支持 admin 页面即时修改。

## 变更内容

- `db/migrations`：新增 `llm_providers` 与 `llm_model_routes`，并同步初始 schema。
- `apps/admin-api`：新增 provider/model CRUD、public model list、默认模型设置、API key AES-GCM 密文入库和脱敏返回。
- `apps/llm-gateway`：新增 Postgres 动态 registry source，优先读取 admin 配置，未配置时保留 JSON/env fallback。
- `apps/web`：新增 `/admin/models` 管理页、admin BFF routes，并让 `/api/models` 改读 admin-api public 模型列表；后台无模型时显示 no model 空态并禁止发送对话；常见 Simple Icons SVG 下载到本地 `public/brands` 并优先用于模型 logo。
- `deploy` / `scripts`：透传 `COCOLA_MODEL_SECRET_KEY` 给 admin-api 和 llm-gateway；dev 默认值仅用于本地。

## 关键取舍

- v1 保存即生效，不做草稿/发布流。
- logo 支持本地内置 Simple Icons slug 或 HTTPS 图片 URL，不做上传；Simple Icons 缺失的 slug 保留文字回退。
- API key 更新只接受新值，读取接口永不返回明文。
