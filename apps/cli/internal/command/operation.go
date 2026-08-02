package command

import (
	"errors"

	"github.com/cocola-project/cocola/apps/cli/internal/config"
	"github.com/cocola-project/cocola/apps/cli/internal/operationlock"
)

func withOperationLock(paths config.Paths, command string, operation func() error) (err error) {
	lock, err := operationlock.Acquire(paths.Home, command)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Close())
	}()
	return operation()
}
