# fix: prevent admins from changing their own permissions

- 变更时间：2026-07-04 15:04 (+08:00)

## 变更理由

管理员用户页此前只保护 bootstrap admin，普通管理员仍可能对自己的角色、启用状态或删除语义发起操作。这样会导致管理员误降权、误禁用或删除自己，造成当前会话权限状态和后台用户状态不一致。

## 变更内容

- apps/admin-api/internal/service/admin.go：新增自操作权限保护，禁止 actor 对自己修改 role、enabled 或删除账号。
- apps/admin-api/internal/httpapi/api.go：新增 `SELF_PERMISSION_CHANGE` 错误码，返回 403。
- apps/admin-api/internal/service/auth_users_test.go：覆盖管理员不能自降权、自禁用、自删除，以及其他管理员仍可操作。
- apps/admin-api/internal/httpapi/api_test.go：覆盖 HTTP 层自操作权限错误码。
- apps/web/app/admin/users/page.tsx：管理员用户页禁用当前登录用户自己的角色、启停、删除按钮；PATCH 只发送实际变更字段。
- 验证：`pnpm --filter @cocola/web lint`、`GOCACHE=/private/tmp/cocola-go-build-cache go test ./...`。
