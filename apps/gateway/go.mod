module github.com/cocola-project/cocola/apps/gateway

go 1.22

require (
	github.com/cocola-project/cocola/packages/go-common v0.0.0
	github.com/cocola-project/cocola/packages/proto/gen/go v0.0.0
	google.golang.org/grpc v1.62.1
)

require (
	github.com/golang/protobuf v1.5.3 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/net v0.20.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240123012728-ef4313101c80 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/cocola-project/cocola/packages/go-common => ../../packages/go-common

replace github.com/cocola-project/cocola/packages/proto/gen/go => ../../packages/proto/gen/go
