// sandbox-manager owns the SandboxProvider abstraction. It receives Create/Exec/
// Destroy gRPC calls from agent-runtime and dispatches to a concrete provider:
//   - DockerProvider (M1)
//   - K8sGVisorProvider (M6)
//   - E2BProvider / CubeSandboxProvider (community)
//
// M0 only registers a no-op provider and exits.
package main

import (
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

func main() {
	log := logger.Must()
	defer func() { _ = log.Sync() }()
	log.Info("cocola sandbox-manager (M0 stub) starting")
}
