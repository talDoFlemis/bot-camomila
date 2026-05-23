// Package app is the composition root for bot-camomila. It wires together
// the config package and the whatsappadapter package.
// Phase 1 Plan 01: stub that compiles and accepts the correct signature.
// The real wiring will be added in Phase 1 Plan 04.
package app

import (
	"context"
	"log/slog"
	"time"
)

// Run is the composition-root entry point. It wires config loading,
// hot-reload, and the WhatsApp adapter, then blocks until ctx is cancelled.
// startTime must be recorded before any whatsmeow operations — it is used
// to filter out HistorySync-replayed messages (D-07).
func Run(ctx context.Context, configPath string, startTime time.Time) error {
	slog.Info("starting bot", "config_path", configPath, "start_time", startTime)

	// Block until shutdown signal.
	<-ctx.Done()

	slog.Info("bot stopped")
	return nil
}
