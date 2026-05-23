// Package main is the binary entrypoint for bot-camomila.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/taldoflemis/bot-camomila/internal/app"
)

func main() {
	levelVar := setupLogging()

	// D-04: --config flag (default "./config.yaml"); BOT_CONFIG env var as fallback.
	// Flag wins over env var.
	configPath := flag.String("config", "./config.yaml", "path to YAML config file")
	flag.Parse()
	if *configPath == "./config.yaml" {
		if v := os.Getenv("BOT_CONFIG"); v != "" {
			*configPath = v
		}
	}

	// D-07: Record start time before any whatsmeow operations. The adapter uses
	// this to filter out HistorySync-replayed messages on first QR pair.
	startTime := time.Now()

	// Signal-aware context — cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := app.Run(ctx, *configPath, startTime, levelVar); err != nil {
		slog.Error("bot exited with error", "err", err)
		os.Exit(1)
	}
}

// setupLogging selects a slog handler based on whether stdout is a terminal.
// Text handler (LevelDebug) for interactive sessions; JSON handler (LevelInfo)
// for Docker / CI / non-TTY environments. Returns a LevelVar so callers can
// change the log level at runtime (e.g. via config hot-reload).
func setupLogging() *slog.LevelVar {
	var levelVar slog.LevelVar
	var handler slog.Handler
	if isatty.IsTerminal(os.Stdout.Fd()) {
		levelVar.Set(slog.LevelDebug)
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: &levelVar, AddSource: true})
	} else {
		levelVar.Set(slog.LevelInfo)
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &levelVar, AddSource: true})
	}
	slog.SetDefault(slog.New(handler))
	return &levelVar
}
