module github.com/cocola-project/cocola/apps/gateway

go 1.23.0

require (
	github.com/cocola-project/cocola/packages/go-common v0.0.0
	github.com/cocola-project/cocola/packages/proto/gen/go v0.0.0
	google.golang.org/grpc v1.62.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240123012728-ef4313101c80 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace github.com/cocola-project/cocola/packages/go-common => ../../packages/go-common

replace github.com/cocola-project/cocola/packages/proto/gen/go => ../../packages/proto/gen/go
