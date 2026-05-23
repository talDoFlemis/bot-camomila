// Package whatsappadapter - walog.go bridges stdlib slog to the waLog.Logger interface
// required by go.mau.fi/whatsmeow.
package whatsappadapter

import (
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// slogAdapter adapts a *slog.Logger to the waLog.Logger interface.
type slogAdapter struct {
	log *slog.Logger
}

// Compile-time interface check — fails at build time if slogAdapter is incomplete.
var _ waLog.Logger = slogAdapter{}

func (a slogAdapter) Debugf(msg string, args ...interface{}) {
	a.log.Debug(fmt.Sprintf(msg, args...))
}

func (a slogAdapter) Infof(msg string, args ...interface{}) {
	a.log.Info(fmt.Sprintf(msg, args...))
}

func (a slogAdapter) Warnf(msg string, args ...interface{}) {
	a.log.Warn(fmt.Sprintf(msg, args...))
}

func (a slogAdapter) Errorf(msg string, args ...interface{}) {
	a.log.Error(fmt.Sprintf(msg, args...))
}

// Sub returns a new Logger scoped to the given sub-module name.
func (a slogAdapter) Sub(module string) waLog.Logger {
	return slogAdapter{log: a.log.With("module", module)}
}

// newWALogger returns a waLog.Logger backed by slog.Default() with the given module label.
func newWALogger(module string) waLog.Logger {
	return slogAdapter{log: slog.Default().With("module", module)}
}
