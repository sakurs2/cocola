module github.com/cocola-project/cocola/apps/admin-api

go 1.22

require (
	github.com/cocola-project/cocola/packages/go-common v0.0.0
	github.com/go-chi/chi/v5 v5.2.0
	github.com/redis/go-redis/v9 v9.7.0
)

require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
)

replace github.com/cocola-project/cocola/packages/go-common => ../../packages/go-common
