package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches the config file for changes and reloads it into the Store.
// It watches the parent directory (not the file directly) to survive atomic-rename
// saves made by editors such as vim and VS Code.
type Watcher struct {
	store      *Store
	configPath string
	levelVar   *slog.LevelVar // nil = caller does not want dynamic level changes
}

// NewWatcher creates a Watcher that will reload cfg into store whenever the file changes.
// levelVar, if non-nil, is updated whenever a reload produces a new log.level value.
func NewWatcher(store *Store, configPath string, levelVar *slog.LevelVar) *Watcher {
	return &Watcher{store: store, configPath: configPath, levelVar: levelVar}
}

// Run starts the watcher loop. It uses fsnotify to watch the parent directory
// with a 200 ms debounce. If fsnotify fails or reports an unrecoverable error,
// Run falls back to polling the file's mtime every 30 seconds.
// Run returns nil when ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("fsnotify unavailable; using mtime poll", "err", err)
		return w.pollOnly(ctx)
	}
	defer fw.Close()

	dir := filepath.Dir(w.configPath)
	base := filepath.Base(w.configPath)

	if err := fw.Add(dir); err != nil {
		slog.Warn("fsnotify: failed to watch config directory; using mtime poll", "dir", dir, "err", err)
		return w.pollOnly(ctx)
	}

	// Record initial mtime for the poll fallback baseline.
	var lastMtime time.Time
	if fi, statErr := os.Stat(w.configPath); statErr == nil {
		lastMtime = fi.ModTime()
	}

	poll := time.NewTicker(30 * time.Second)
	defer poll.Stop()

	var debounce *time.Timer

mainLoop:
	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-fw.Events:
			if !ok {
				slog.Warn("fsnotify events channel closed; switching to mtime poll")
				break mainLoop
			}
			// Filter: only react to events for the config file itself.
			if filepath.Base(ev.Name) != base {
				continue
			}
			// Filter: only react to content-changing operations.
			if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Rename) {
				continue
			}
			// Debounce: reset the timer on every qualifying event.
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(200*time.Millisecond, w.reload)

		case fwErr, ok := <-fw.Errors:
			if !ok || fwErr != nil {
				slog.Warn("fsnotify error; switching to mtime poll", "err", fwErr)
				break mainLoop
			}

		case <-poll.C:
			fi, statErr := os.Stat(w.configPath)
			if statErr == nil && fi.ModTime().After(lastMtime) {
				lastMtime = fi.ModTime()
				w.reload()
			}
		}
	}

	return w.pollOnly(ctx)
}

// pollOnly is the fallback loop used when fsnotify is unavailable or reports an error.
// It polls the config file's mtime every 30 seconds.
func (w *Watcher) pollOnly(ctx context.Context) error {
	var lastMtime time.Time
	if fi, err := os.Stat(w.configPath); err == nil {
		lastMtime = fi.ModTime()
	}

	poll := time.NewTicker(30 * time.Second)
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			fi, err := os.Stat(w.configPath)
			if err == nil && fi.ModTime().After(lastMtime) {
				lastMtime = fi.ModTime()
				w.reload()
			}
		}
	}
}

// reload loads the config file and swaps the snapshot in the store.
// On failure it logs a warning and keeps the current snapshot — the bot keeps running.
func (w *Watcher) reload() {
	snap, err := Load(w.configPath)
	if err != nil {
		slog.Warn("config reload failed; keeping previous config", "err", err)
		return
	}
	w.store.Swap(snap)
	if w.levelVar != nil && snap.LogLevel != nil {
		w.levelVar.Set(*snap.LogLevel)
		slog.Info("log level updated", "level", snap.LogLevel.String())
	}
	slog.Info("config reloaded", "path", w.configPath)
}
