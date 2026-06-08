// sandbox-manager owns the SandboxProvider abstraction. It receives Create/Exec/
// Destroy gRPC calls from agent-runtime and dispatches to a concrete provider:
//   - DockerProvider (M1)
//   - K8sGVisorProvider (later)
//   - E2BProvider / CubeSandboxProvider (community)
//
// The provider is chosen at startup from COCOLA_SANDBOX_PROVIDER (default: docker).
// Nothing below the provider factory knows which backend is in use.
package main

import (
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/provider/docker"
	"github.com/cocola-project/cocola/apps/sandbox-manager/internal/server"
	"github.com/cocola-project/cocola/packages/go-common/logger"
	sandboxv1 "github.com/cocola-project/cocola/packages/proto/gen/go/cocola/sandbox/v1"
)

func main() {
	log := logger.Must()
	defer func() { _ = log.Sync() }()

	addr := getenv("COCOLA_SANDBOX_ADDR", ":50051")
	backend := getenv("COCOLA_SANDBOX_PROVIDER", docker.ProviderName)

	p, err := newProvider(backend)
	if err != nil {
		log.Sugar().Fatalf("init provider %q: %v", backend, err)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Sugar().Fatalf("listen %s: %v", addr, err)
	}

	gs := grpc.NewServer()
	sandboxv1.RegisterSandboxServiceServer(gs, server.New(p))
	reflection.Register(gs) // enables grpcurl describe/list for local debugging

	log.Sugar().Infow("cocola sandbox-manager listening",
		"milestone", "M1", "addr", addr, "provider", backend)
	if err := gs.Serve(lis); err != nil {
		log.Sugar().Fatalf("serve: %v", err)
	}
}

// newProvider is the single place that maps a backend name to a concrete
// implementation. Adding a new backend = one case here + a package under
// internal/provider/. No other file changes.
func newProvider(name string) (provider.SandboxProvider, error) {
	switch name {
	case docker.ProviderName:
		return docker.New()
	default:
		// Allow providers that self-registered via Register() in their init().
		if p := provider.Get(name); p != nil {
			return p, nil
		}
		return docker.New()
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
