# feat(m7): admin-api Postgres 后端 + goose 迁移嵌入

M7 持久化第三步:admin-api 落地 Postgres 实现,与内存实现行为对拍一致;DSN
未设置时默认仍走内存,保零依赖 dev 启动。

## 改动

- `db/`(新增 tiny Go module,schema 单一真相载体):
  - `db/go.mod`:`github.com/cocola-project/cocola/db`。
  - `db/embed.go`:`//go:embed migrations/*.sql` 暴露 `Migrations` 给 Go 侧作为
    goose 源;Python 侧只连库不重定义 schema。
- `apps/admin-api/internal/store/postgres.go`(新增):`Postgres` 实现完整
  `store.Store`(tokens / quota / skills / audit);唯一冲突 → `ErrConflict`,
  缺行 → `ErrNotFound`,NULL 时间映射零值,语义与 `Memory` 对齐。
- `apps/admin-api/internal/store/migrate.go`(新增):`Migrate` 用 pgx stdlib
  驱动 + goose `UpContext` 幂等 apply 嵌入迁移。
- `apps/admin-api/internal/store/parity_test.go`(新增):同一 `runStoreContract`
  同时跑 Memory 与 Postgres;PG 腿由 `COCOLA_TEST_PG_DSN` 门控,未设置即 skip。
- `apps/admin-api/cmd/admin-api/main.go`:`COCOLA_PG_DSN` 设置则迁移 + 接 PG,
  否则回退内存(handler 无感)。
- `apps/admin-api/Dockerfile`:构建阶段 COPY `db/`(replace 指向 ../../db)。
- `apps/admin-api/go.mod` / `go.work`:引入 pgx/v5、goose/v3、本地 db module。
- 依赖锁定 go1.24 兼容版本:pgx v5.7.2、goose v3.24.1。

## 测试

- `go test ./...`(admin-api)全绿;`go vet` 通过;`gofmt -l -s` 无报告。
- Postgres 对拍腿:对真实 postgres:16-alpine 实跑,goose 成功迁移到 version 1,
  `TestStoreContract_Postgres_Parity` 与 Memory 腿断言完全一致并通过。
- `GOWORK=off` 独立模块构建(等价 Dockerfile)通过。
