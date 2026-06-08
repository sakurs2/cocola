// Package logger provides a thin wrapper over zap to keep service code free of
// direct zap imports. Swap the backend here (zerolog, slog) without touching callers.
package logger

import (
	"go.uber.org/zap"
)

// Logger is the public alias exposed to services.
type Logger = *zap.Logger

// New constructs a production-grade logger. In M0 we keep it intentionally simple;
// configuration (level, sink, sampling) will be wired through go-common/config in M3.
func New() (Logger, error) {
	return zap.NewProduction()
}

// Must is a convenience wrapper for the common case where logger init failure
// should abort process startup.
func Must() Logger {
	l, err := New()
	if err != nil {
		panic(err)
	}
	return l
}
