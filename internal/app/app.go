// Package app is the composition root for bot-camomila. It wires together
// the config package and the whatsappadapter package into a single runnable unit.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/taldoflemis/bot-camomila/internal/config"
	"github.com/taldoflemis/bot-camomila/internal/cooldown"
	"github.com/taldoflemis/bot-camomila/internal/domain"
	"github.com/taldoflemis/bot-camomila/internal/killswitch"
	"github.com/taldoflemis/bot-camomila/internal/pipeline"
	"github.com/taldoflemis/bot-camomila/internal/whatsappadapter"
)

// Run is the composition-root entry point. It wires config loading,
// hot-reload, and the WhatsApp adapter, then blocks until ctx is cancelled.
// startTime must be recorded before any whatsmeow operations — it is used
// to filter out HistorySync-replayed messages (D-07).
// levelVar, if non-nil, is updated whenever a reload produces a new log.level value.
func Run(ctx context.Context, configPath string, startTime time.Time, levelVar *slog.LevelVar) error {
	// Step 1 — Load initial config.
	snap, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("initial config load failed: %w", err)
	}

	// Apply log level from config if set, before any further logging.
	if levelVar != nil && snap.LogLevel != nil {
		levelVar.Set(*snap.LogLevel)
	}

	slog.Info("starting bot",
		"config_path", configPath,
		"start_time", startTime.Format(time.RFC3339),
	)

	// Step 2 — Create atomic config store.
	cfgStore := config.NewStore(snap)

	// Step 3 — Start config watcher in background goroutine.
	watcher := config.NewWatcher(cfgStore, configPath, levelVar)
	go func() {
		if err := watcher.Run(ctx); err != nil {
			slog.Error("config watcher exited with error", "err", err)
		}
	}()

	// Step 4 — Initialize channels.
	inCh := make(chan domain.InboundMessage, 64)
	outCh := make(chan domain.OutboundReply, 64)

	// Step 5 — Create WhatsApp adapter.
	// adapter.New() records startTime internally (time.Now() in New).
	// The startTime parameter to Run() is for app-level logging only.
	adapter := whatsappadapter.New(cfgStore, inCh, outCh)

	// Step 6 — Create and start Phase 2 pipeline components.
	ks := killswitch.New()
	cd := cooldown.NewTracker(nil) // nil = real clock (time.Now)
	rl := pipeline.NewRateLimiter(nil)
	pipe := pipeline.New(cfgStore, adapter, ks, cd, rl, nil) // adapter implements domain.AdminChecker

	// Start cooldown reaper in background (cleanup every 5 minutes).
	go cd.StartReaper(ctx, 5*time.Minute)

	// Start pipeline actor loop.
	go pipe.Run(ctx, inCh, outCh)

	slog.Info("pipeline created",
		"kill_switch", "active",
		"cooldown_reaper_interval", "5m",
	)

	// Step 7 — Start adapter ReplyLoop.
	go adapter.ReplyLoop(ctx)

	// Step 8 — Start adapter connection.
	if err := adapter.Start(ctx); err != nil {
		return fmt.Errorf("whatsapp adapter start failed: %w", err)
	}

	// Step 9 — Block on context until shutdown signal.
	<-ctx.Done()
	slog.Info("shutdown signal received; disconnecting")

	// Step 10 — Graceful shutdown.
	// Order is mandatory: adapter.Disconnect() (calls client.Disconnect then db.Close).
	// NEVER call Disconnect() from inside an event handler — deadlock.
	adapter.Disconnect()
	slog.Info("bot stopped")
	return nil
}
