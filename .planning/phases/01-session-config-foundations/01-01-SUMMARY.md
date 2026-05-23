---
phase: 01-session-config-foundations
plan: 01
subsystem: infra
tags: [go, whatsmeow, sqlite, modernc, goccy-yaml, fsnotify, isatty, slog, hexagonal]

# Dependency graph
requires: []
provides:
  - Hexagonal Go project layout with cmd/bot/ and internal/{app,config,domain,whatsappadapter}/ packages
  - Pinned go.mod with all five production dependencies at exact versions
  - Config, Snapshot, and all sub-config struct types with correct yaml tags
  - transport-agnostic domain.Message type (stdlib-only, no whatsmeow)
  - Binary entrypoint with TTY-based slog handler selection and --config/BOT_CONFIG flag+env parsing
  - composition-root app.Run(ctx, configPath, startTime) stub
  - sqlite3 dialect alias registration in whatsappadapter init()
affects: [01-02, 01-03, 01-04]

# Tech tracking
tech-stack:
  added:
    - go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69
    - modernc.org/sqlite@v1.50.1
    - github.com/goccy/go-yaml@v1.19.2
    - github.com/fsnotify/fsnotify@v1.10.1
    - github.com/mattn/go-isatty@v0.0.22
  patterns:
    - Hexagonal architecture: whatsappadapter is the only whatsmeow importer
    - atomic.Pointer[Snapshot] config snapshot pattern (type definitions ready)
    - TTY-based slog handler selection (text=dev, JSON=Docker/CI)
    - --config flag with BOT_CONFIG env fallback (flag wins)
    - startTime recorded in main() before app.Run for HistorySync timestamp gate

key-files:
  created:
    - go.mod
    - go.sum
    - cmd/bot/main.go
    - internal/app/app.go
    - internal/config/config.go
    - internal/config/load.go
    - internal/domain/message.go
    - internal/whatsappadapter/adapter.go
  modified: []

key-decisions:
  - "hexagonal layout from day one: internal/whatsappadapter is the only whatsmeow importer"
  - "modernc.org/sqlite dialect alias registered in whatsappadapter init() as sqlite3 for sqlstore"
  - "startTime passed from main() through app.Run to adapter for HistorySync flood filter (D-07)"
  - "config/load.go stub created alongside config.go to keep goccy/go-yaml and fsnotify in go.mod"

patterns-established:
  - "Pattern 1 (sqlite3 alias): sql.Register('sqlite3', &sqlite.Driver{}) in whatsappadapter init()"
  - "Pattern 2 (TTY slog): isatty.IsTerminal(os.Stdout.Fd()) selects text/JSON handler before any slog call"
  - "Pattern 3 (flag+env): flag wins over BOT_CONFIG env var; check env only when flag is default"
  - "Pattern 4 (hexagonal boundary): domain.Message imports only stdlib; no whatsmeow types leak out"

requirements-completed: [SESSION-01, SESSION-02, CONFIG-01, OBSERV-01]

# Metrics
duration: 5min
completed: 2026-05-23
---

# Phase 01 Plan 01: Bootstrap Summary

**Hexagonal Go module scaffold with all five pinned dependencies, Config/Snapshot types, domain.Message, isatty-based slog entrypoint, and sqlite3 dialect alias establishing the type contracts for all Wave 2 plans**

## Performance

- **Duration:** 5 min
- **Started:** 2026-05-23T10:19:39Z
- **Completed:** 2026-05-23T10:24:43Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- Hexagonal directory layout created: `cmd/bot/`, `internal/{app,config,domain,whatsappadapter}/`
- All five production dependencies pinned in `go.mod` at exact RESEARCH.md versions; `go build ./...` and `go vet ./...` both exit 0
- `internal/config/config.go` defines all Config/Snapshot types with yaml tags matching `config.example.yaml`
- `internal/domain/message.go` exports `Message` with zero external imports (hexagonal boundary enforced)
- `cmd/bot/main.go` wires TTY-based slog handler, --config/BOT_CONFIG flag, signal context, and `app.Run`
- `internal/whatsappadapter/adapter.go` registers `sqlite3` dialect alias in `init()` and imports whatsmeow

## Task Commits

Each task was committed atomically:

1. **Task 1: Add dependencies to go.mod and create hexagonal directory layout** - `8c91239` (chore)
2. **Task 2: Write Config/Snapshot types, domain.Message, and entrypoint stubs** - `4c6c18e` (feat)

**Plan metadata:** (docs commit below)

## Files Created/Modified

- `go.mod` - Module declaration with all five pinned production dependencies
- `go.sum` - Checksum database for dependency verification
- `cmd/bot/main.go` - Binary entrypoint: setupLogging(), --config/BOT_CONFIG, signal context, app.Run
- `internal/app/app.go` - composition-root Run(ctx, configPath, startTime) stub; logs start/stop, blocks on ctx.Done
- `internal/config/config.go` - Config, Snapshot, ResolvedMatcher, and all sub-config types (yaml tags)
- `internal/config/load.go` - Load() and validate() stubs importing goccy/go-yaml and fsnotify (keeps deps in go.mod)
- `internal/domain/message.go` - transport-agnostic Message struct; imports only "time"
- `internal/whatsappadapter/adapter.go` - init() registers sqlite3 alias; Adapter stub imports whatsmeow

## Decisions Made

- `config/load.go` stub created alongside `config.go` to keep `goccy/go-yaml` and `fsnotify` in `go.mod` (go mod tidy removes unused deps; load.go references both packages so they stay as direct deps until Plan 02 implements them fully)
- `whatsappadapter/adapter.go` stub includes an `Adapter` struct that holds `*whatsmeow.Client` to keep `whatsmeow` as a direct dependency before Plan 03 implements the full adapter
- `startTime` recorded in `main()` before `app.Run` is called (safer than inside adapter; any message arriving before startTime is recorded would not be filtered — main() entry is the earliest safe point)
- `sqlite3` dialect alias registered in `init()` per RESEARCH.md Pattern 1 (CLAUDE.md critical invariant)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added config/load.go and whatsappadapter/adapter.go stubs**
- **Found during:** Task 2 (go mod tidy after writing source files)
- **Issue:** `go mod tidy` removed `goccy/go-yaml`, `fsnotify`, and `whatsmeow` from go.mod because no source file imported them. The plan requires all five deps in go.mod.
- **Fix:** Created `internal/config/load.go` with real Load/validate stubs importing `goccy/go-yaml` and `fsnotify`, and `internal/whatsappadapter/adapter.go` with an `Adapter` stub importing `whatsmeow`. Both stubs are correct partial implementations (not dead code) that will be extended in Plans 02 and 03.
- **Files modified:** `internal/config/load.go` (new), `internal/whatsappadapter/adapter.go` (new)
- **Verification:** `go mod tidy` retains all five deps; `go build ./...` exits 0
- **Committed in:** `4c6c18e` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (Rule 2 - missing critical: deps removed by go mod tidy)
**Impact on plan:** Auto-fix is additive only; both stub files are correct partial implementations that Plans 02 and 03 will extend. No scope creep; all plan success criteria met.

## Issues Encountered

- `go mod tidy` removed the four deps (whatsmeow, sqlite, go-yaml, fsnotify) when run before any source files importing them existed. Resolved by writing source stubs first, then running tidy. Not a blocking issue.

## User Setup Required

None - no external service configuration required for this plan. WhatsApp QR pairing is a prerequisite for Plans 03-04, not this plan.

## Next Phase Readiness

- `go build ./...` and `go vet ./...` pass; module compiles cleanly
- All five dependencies pinned at exact RESEARCH.md versions
- Type contracts (`Config`, `Snapshot`, `ResolvedMatcher`, `Message`) are established for Plans 02-04
- `cmd/bot/main.go` entrypoint stub is correct and will not need structural changes in Plans 02-04
- Plan 02 (config load+validate+store+watcher) can start immediately; extends `load.go` and adds `store.go`/`watcher.go`
- Plan 03 (whatsappadapter) can start immediately; extends `adapter.go` and adds `client.go`/`inbound.go`/`walog.go`

---
*Phase: 01-session-config-foundations*
*Completed: 2026-05-23*
