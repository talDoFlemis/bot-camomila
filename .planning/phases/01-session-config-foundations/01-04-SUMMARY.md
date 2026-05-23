---
phase: 01-session-config-foundations
plan: "04"
subsystem: app-wiring
tags: [composition-root, wiring, graceful-shutdown, config, whatsappadapter]
dependency_graph:
  requires:
    - 01-02 (config package: Load, Store, Watcher)
    - 01-03 (whatsappadapter: New, Start, Disconnect)
  provides:
    - app.Run() composition root
    - complete Phase 1 walking skeleton
  affects:
    - cmd/bot/main.go (dead-code removal)
    - internal/app/app.go (full implementation)
tech_stack:
  added: []
  patterns:
    - atomic config store (load-once per handler call)
    - goroutine-wrapped watcher with error logging
    - graceful shutdown via ctx.Done then Disconnect
key_files:
  created: []
  modified:
    - internal/app/app.go
    - cmd/bot/main.go
decisions:
  - "app.Run() blocks on ctx.Done() before calling adapter.Disconnect() — never from event handler (deadlock prevention)"
  - "startTime parameter logged at app level; adapter records its own time.Now() in New()"
metrics:
  duration: "~5 minutes"
  completed: "2026-05-23"
  tasks_completed: 1
  tasks_total: 2
  files_changed: 2
---

# Phase 1 Plan 04: Entrypoint Wiring Summary

**One-liner:** app.Run() wires config.Load + atomic Store + fsnotify Watcher + whatsappadapter into a graceful-shutdown composition root.

## What Was Built

`internal/app/app.go` — full implementation of `Run(ctx, configPath, startTime)`:

1. `config.Load(configPath)` — fail-fast if initial config is invalid (T-04-02 mitigation)
2. `config.NewStore(snap)` — atomic snapshot for concurrent hot-reload
3. `config.NewWatcher(cfgStore, configPath)` in background goroutine — watcher errors logged at ERROR, bot continues
4. `whatsappadapter.New(cfgStore)` + `adapter.Start(ctx)` — QR pair or session resume
5. `<-ctx.Done()` block — waits for SIGTERM/SIGINT
6. `adapter.Disconnect()` — clean WhatsApp client shutdown then db.Close()

`cmd/bot/main.go` — removed dead code (unreachable `<-ctx.Done()` and duplicate "bot stopped" log that appeared after `app.Run()` already handled shutdown).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Removed unreachable dead code from main.go**
- **Found during:** Task 1
- **Issue:** `main.go` had `<-ctx.Done()` and `slog.Info("bot stopped")` after `app.Run()` returned. Since `app.Run()` itself blocks on `<-ctx.Done()` and logs "bot stopped" before returning, the post-return code was unreachable dead code that would have produced a duplicate log line if ever reached.
- **Fix:** Removed the trailing `<-ctx.Done()` and `slog.Info("bot stopped")` from `main.go`.
- **Files modified:** `cmd/bot/main.go`
- **Commit:** b87b365

## Task Commits

| Task | Description | Commit | Files |
|------|-------------|--------|-------|
| 1 | Implement app.Run() composition root | b87b365 | internal/app/app.go, cmd/bot/main.go |

## Checkpoint Task

Task 2 is `type="checkpoint:human-verify"` — requires operator to build the binary, scan the QR code, and verify all 7 steps of the walking skeleton (session persistence, message gating, graceful shutdown, hot-reload).

## Threat Model Coverage

| Threat | Disposition | Status |
|--------|-------------|--------|
| T-04-01: Shutdown ordering | mitigate | adapter.Disconnect() called after ctx.Done() — never from event handler |
| T-04-02: Initial config load failure | mitigate | app.Run() returns error immediately; main.go exits non-zero |
| T-04-03: Watcher goroutine panic | mitigate | watcher.Run() error logged at ERROR; bot continues |

## Known Stubs

None — app.Run() is fully wired with real implementations from Plans 02 and 03.

## Self-Check: PASSED

- [x] internal/app/app.go exists and contains all required call sites
- [x] cmd/bot/main.go modified (dead code removed)
- [x] Commit b87b365 exists: `git log --oneline | grep b87b365`
- [x] go build ./... exits 0
- [x] go vet ./... exits 0
- [x] grep adapter.Disconnect internal/app/app.go — found (after ctx.Done())
- [x] grep ctx.Done internal/app/app.go — found
- [x] grep config.Load internal/app/app.go — found
- [x] grep config.NewStore internal/app/app.go — found
- [x] grep config.NewWatcher internal/app/app.go — found
- [x] grep whatsappadapter.New internal/app/app.go — found
- [x] grep adapter.Start internal/app/app.go — found
