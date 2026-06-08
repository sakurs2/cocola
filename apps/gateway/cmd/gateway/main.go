// gateway is the public-facing BFF. Responsibilities:
//   - HTTP/WebSocket termination from the web client
//   - Auth (delegates to admin-api in M4)
//   - Routing requests to agent-runtime via gRPC
//   - Streaming agent events back over SSE/WS
//
// M0 only proves wiring: print a banner and exit cleanly.
package main

import (
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

func main() {
	log := logger.Must()
	defer func() { _ = log.Sync() }()
	log.Info("cocola gateway (M0 stub) starting")
}
