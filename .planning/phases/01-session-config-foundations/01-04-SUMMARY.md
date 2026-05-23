---
phase: 01-session-config-foundations
plan: "04"
subsystem: app-wiring
tags: [composition-root, wiring, graceful-shutdown, config, whatsappadapter]

requires:
  - phase: 01-02
    provides: config.Load, config.NewStore, config.NewWatcher — atomic snapshot with fsnotify hot-reload
  - phase: 01-03
    provides: whatsappadapter.New, adapter.Start, adapter.Disconnect — WhatsApp lifecycle and event gating
provides:
  - app.Run() composition root wiring all subsystems into a runnable bot
  - Graceful shutdown sequence: adapter.Disconnect() after ctx.Done() within 10s
  - Operator-verified Phase 1 walking skeleton (all 7 verification steps passed)
affects: [02-matcher-pipeline, 03-owner-commands, 04-docker-packaging]

tech-stack:
  added: []
  patterns:
    - "Composition root: app.Run() wires all subsystems; main.go handles only slog + flags + signal"
    - "Config load-first: app.Run() fails fast on initial config error before starting any subsystem"
    - "Shutdown ordering: <-ctx.Done() → adapter.Disconnect() → db.Close() (never from event handler)"

key-files:
  created: []
  modified:
    - internal/app/app.go
    - cmd/bot/main.go

key-decisions:
  - "app.Run() blocks on ctx.Done() before calling adapter.Disconnect() — never from event handler (deadlock prevention)"
  - "startTime parameter logged at app level; adapter records its own time.Now() in New()"

requirements-completed:
  - SESSION-01
  - SESSION-02
  - SESSION-03
  - SESSION-04
  - SESSION-05
  - CONFIG-01
  - CONFIG-02
  - CONFIG-03
  - CONFIG-04
  - CONFIG-05
  - SCOPE-01
  - SCOPE-02
  - SCOPE-03
  - OBSERV-01
  - OBSERV-03

duration: "~92 min (including operator verification)"
completed: "2026-05-23"
---

# Phase 1 Plan 04: Entrypoint Wiring Summary

**app.Run() composition root wiring config hot-reload, whatsappadapter, and graceful shutdown — Phase 1 walking skeleton operator-verified across all 7 steps**

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

## Operator Verification

Task 2 was `type="checkpoint:human-verify"`. Operator verified all 7 steps and approved:

1. Binary builds with `go build -o bot ./cmd/bot/` — passed
2. First launch prints QR code; scan with phone — passed
3. Session persists: restart without QR — passed
4. Group messages produce structured `"message received"` log — passed
5. Non-group messages and self-sent messages are silently dropped — passed
6. `SIGTERM` produces clean shutdown within 10s, exit code 0 — passed
7. Config hot-reload fires within ~500ms; invalid reload keeps prior snapshot with WARN — passed

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
