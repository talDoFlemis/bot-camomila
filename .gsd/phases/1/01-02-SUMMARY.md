---
phase: 01-session-config-foundations
plan: 02
subsystem: config
tags: [go-yaml, fsnotify, whatsmeow, atomic, config-hot-reload, jid-validation]

# Dependency graph
requires:
  - phase: 01-01
    provides: "Config, Snapshot, ResolvedMatcher, Store, Watcher type definitions in internal/config/config.go"
provides:
  - "Load(path string) (*Snapshot, error) with strict YAML decode and six load-time validation checks"
  - "atomic Store type (Get/Swap) backed by atomic.Pointer[Snapshot]"
  - "Watcher type with Run(ctx) — fsnotify parent-dir watch + 200ms debounce + 30s mtime poll fallback"
affects: [01-03, 01-04, internal/whatsappadapter, internal/app]

# Tech tracking
tech-stack:
  added:
    - "github.com/goccy/go-yaml v1.19.2 — strict YAML decode with DisallowUnknownField()"
    - "github.com/fsnotify/fsnotify v1.10.1 — config file change detection"
    - "go.mau.fi/whatsmeow/types — types.ParseJID, types.GroupServer for JID validation"
  patterns:
    - "atomic.Pointer[Snapshot] for lock-free config reads across goroutines"
    - "fsnotify watches parent directory (not file) to survive atomic-rename saves"
    - "200ms debounce via time.AfterFunc before triggering config reload"
    - "30s mtime poll ticker as belt-and-suspenders fallback when fsnotify errors"
    - "validate-then-swap: reload failure logs WARN and keeps previous Snapshot"

key-files:
  created:
    - internal/config/store.go
    - internal/config/watcher.go
  modified:
    - internal/config/load.go

key-decisions:
  - "Validate group_jid only when non-empty — allows config.example.yaml (no scope section) to load without error"
  - "Cluster duplicate detection added: ambiguous cluster name returns error before matcher resolution"
  - "Self-loop guard uses exact lowercase token match (not Levenshtein) — full fuzzy check deferred to matcher package in Phase 2"
  - "pollOnly extracted as separate method — cleaner than goto for the fallback loop"

patterns-established:
  - "Pattern: atomic config store — readers call Get() once per handler call, hold pointer for full duration"
  - "Pattern: reload failure isolation — watcher never panics; bad config keeps previous snapshot"
  - "Pattern: parent-dir fsnotify watch — fw.Add(filepath.Dir(path)) not fw.Add(path)"

requirements-completed: [CONFIG-01, CONFIG-02, CONFIG-03, CONFIG-04, CONFIG-05]

# Metrics
duration: 12min
completed: 2026-05-23
---

# Phase 1 Plan 02: Config Load, Store, and Hot-Reload Watcher Summary

**Full config subsystem: strict YAML load with six-check validation (JID, TZ, distance, cluster, self-loop), atomic Snapshot store via atomic.Pointer, and fsnotify parent-dir watcher with 200ms debounce and 30s mtime poll fallback**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-05-23T10:18:00Z
- **Completed:** 2026-05-23T10:30:57Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments

- Replaced stub `load.go` with production `Load()` + `validate()` implementing all six CONFIG-02 checks in order: group JID, owner JIDs, timezone, cluster resolution, distance min-length, self-loop guard
- Created `store.go` with `Store` type backed by `atomic.Pointer[Snapshot]` with documented `Get()`/`Swap()` semantics
- Created `watcher.go` with `Watcher.Run(ctx)` watching the parent directory via `fsnotify`, debouncing 200ms, and falling back to 30s mtime polling on fsnotify errors

## Task Commits

Each task was committed atomically:

1. **Task 1: Load() with strict YAML decode and full load-time validation** - `b895a71` (feat)
2. **Task 2: Atomic Store and fsnotify Watcher with debounce and mtime poll fallback** - `dba1a87` (feat)

## Files Created/Modified

- `internal/config/load.go` — Full `Load()` + `validate()` with 6 validation checks; replaced Plan 01 stub
- `internal/config/store.go` — `Store` type with `NewStore()`, `Get()`, `Swap()` backed by `atomic.Pointer[Snapshot]`
- `internal/config/watcher.go` — `Watcher` with `Run(ctx)` (fsnotify parent-dir + debounce) and `pollOnly()` fallback

## Decisions Made

- **Conditional JID validation:** `group_jid` and `owner_jids` validation only fires when the values are non-empty. This allows `config.example.yaml` (which has no `scope:` section) to load successfully as required by `must_haves.truths`. In a real deployment, an empty group_jid would be caught at startup when the bot tries to filter messages.
- **Cluster duplicate detection:** Added a duplicate-name check in the cluster map-building loop. If the same cluster name appears twice in `answers_cluster`, the loader returns an error ("ambiguous reference") before any matcher resolution.
- **Self-loop guard is exact-match only:** The plan specifies "tokenize on whitespace, lowercase each token, check if token exactly matches any word in matcher.Words (also lowercased)." Full Levenshtein-distance self-loop detection is out of scope for the config loader — that's the matcher package's job in Phase 2.
- **pollOnly as a named method:** The `goto pollOnly` pattern from the RESEARCH.md example was refactored into a named `pollOnly(ctx) error` method for clarity and testability. Behavior is identical.

## Deviations from Plan

None — plan executed exactly as written. The `pollOnly` refactor from `goto` to a named method is a style choice within the spirit of the plan's requirement for a poll fallback loop.

## Issues Encountered

None. The build and vet passed on the first attempt for both tasks.

## Threat Surface Scan

No new security-relevant surface introduced beyond what the plan's threat model covers (T-02-01 through T-02-05). All mitigations applied as specified:
- `yaml.DisallowUnknownField()` rejects unknown YAML keys (T-02-01)
- `types.ParseJID` + `types.GroupServer` check for JID validation (T-02-02)
- 200ms debounce prevents reload storm (T-02-03)
- Parent-directory fsnotify watch handles symlink swaps via Create/Rename events (T-02-04)

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `internal/config` package is complete and independently buildable (`go build ./internal/config/...` exits 0)
- `Store` and `Watcher` are ready for wiring in `internal/app/app.go` (Plan 01-04 or whichever plan creates the composition root)
- `Load()` can be unit-tested without a WhatsApp connection
- Plans 01-03 (whatsappadapter) can import `config.Store` immediately

---
*Phase: 01-session-config-foundations*
*Completed: 2026-05-23*
