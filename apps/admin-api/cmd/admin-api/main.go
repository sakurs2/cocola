// admin-api serves the admin console: tenant/user management, token quotas,
// skill market, audit logs. It is the only service that owns the multi-tenant
// PostgreSQL schema directly.
//
// M0 only proves wiring.
package main

import (
	"github.com/cocola-project/cocola/packages/go-common/logger"
)

func main() {
	log := logger.Must()
	defer func() { _ = log.Sync() }()
	log.Info("cocola admin-api (M0 stub) starting")
}
