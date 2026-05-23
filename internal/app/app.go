// Package app is the composition root for bot-camomila. It wires together
// the config package and the whatsappadapter package into a single runnable unit.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/taldoflemis/bot-camomila/internal/config"
	"github.com/taldoflemis/bot-camomila/internal/whatsappadapter"
)

// Run is the composition-root entry point. It wires config loading,
// hot-reload, and the WhatsApp adapter, then blocks until ctx is cancelled.
// startTime must be recorded before any whatsmeow operations — it is used
// to filter out HistorySync-replayed messages (D-07).
func Run(ctx context.Context, configPath string, startTime time.Time) error {
	// Step 1 — Load initial config.
	snap, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("initial config load failed: %w", err)
	}
	slog.Info("starting bot",
		"config_path", configPath,
		"start_time", startTime.Format(time.RFC3339),
	)

	// Step 2 — Create atomic config store.
	cfgStore := config.NewStore(snap)

	// Step 3 — Start config watcher in background goroutine.
	watcher := config.NewWatcher(cfgStore, configPath)
	go func() {
		if err := watcher.Run(ctx); err != nil {
			slog.Error("config watcher exited with error", "err", err)
		}
	}()

	// Step 4 — Create and start the WhatsApp adapter.
	// adapter.New() records startTime internally (time.Now() in New).
	// The startTime parameter to Run() is for app-level logging only.
	adapter := whatsappadapter.New(cfgStore)
	if err := adapter.Start(ctx); err != nil {
		return fmt.Errorf("whatsapp adapter start failed: %w", err)
	}

	// Step 5 — Block on context until shutdown signal.
	<-ctx.Done()
	slog.Info("shutdown signal received; disconnecting")

	// Step 6 — Graceful shutdown.
	// Order is mandatory: adapter.Disconnect() (calls client.Disconnect then db.Close).
	// NEVER call Disconnect() from inside an event handler — deadlock.
	adapter.Disconnect()
	slog.Info("bot stopped")
	return nil
}
