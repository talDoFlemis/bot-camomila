# Phase 1: Session & Config Foundations - Context

**Gathered:** 2026-05-23
**Status:** Ready for planning

<domain>
## Phase Boundary

Connect to WhatsApp via QR pairing, persist the device session in SQLite, load and hot-reload the YAML config, apply the group-JID/IsFromMe/non-text gates, and log all lifecycle events. No matching or replies yet — the bot connects, gates, and logs silently.

</domain>

<decisions>
## Implementation Decisions

### Config YAML Schema
- **D-01:** Use the `answers_cluster` pattern from `config.example.yaml`. Top-level `answers_cluster:` list with named pools; each matcher references a pool by name via `cluster:` key. Config loader resolves cluster → `[]string` answers at load time.
- **D-02:** Config struct sections (beyond `answers_cluster:` and `matchers:`):
  - `scope: { group_jid, owner_jids }` — group identity and owner allowlist
  - `limits: { quiet_hours: { start, end, timezone }, rate_cap: { per_min, per_hour } }` — behavioral limits
  - `log: { format }` — logging preferences
  - `db: { path }` — SQLite session DB path (inside YAML, not a CLI flag)
- **D-03:** CONFIG-02 validation runs at load time: invalid group JID, owner JID parse error, unresolvable timezone, matcher distance below min-word-length (distance 1 → ≥5 chars, distance 2 → ≥8 chars), self-loop guard (answer tokens must not fuzzy-match matcher keywords), and missing or ambiguous cluster reference.

### CLI / Env Interface
- **D-04:** Config path: `--config` CLI flag (default `./config.yaml`) + `BOT_CONFIG` env var fallback. Flag wins over env.
- **D-05:** SQLite DB path: in the YAML under `db: { path: ./session.sqlite }`. Not a CLI flag. No separate `--db` flag.
- **D-06:** No other CLI flags in Phase 1.

### Claude's Discretion
- **Package structure:** `cmd/bot/main.go` + `internal/` packages from day 1 (hexagonal as designed). `internal/whatsappadapter` is the ONLY package importing whatsmeow.
- **Log format selection:** TTY auto-detect via `isatty`. Text handler when stdout is a terminal (dev); JSON handler otherwise (Docker, CI). No `LOG_FORMAT` env var needed.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements
- `.planning/REQUIREMENTS.md` §SESSION-01 – SESSION-05 — pairing, SQLite persist, integrity_check on startup, LoggedOut exit non-zero, disconnect recovery
- `.planning/REQUIREMENTS.md` §CONFIG-01 – CONFIG-05 — YAML load, validation rules, atomic.Pointer snapshot, fsnotify parent-dir + debounce, mtime fallback poll
- `.planning/REQUIREMENTS.md` §SCOPE-01 – SCOPE-03 — group JID hard gate, IsFromMe filter, non-text msg drop
- `.planning/REQUIREMENTS.md` §OBSERV-01, OBSERV-03 — structured slog, lifecycle event logging

### Architecture
- `.planning/research/ARCHITECTURE.md` — package layout, hexagonal boundaries, hot-path lock-free patterns, build order
- `.planning/research/STACK.md` — pinned dependency versions (whatsmeow pseudo-version, modernc.org/sqlite v1.50.1 dialect `"sqlite3"`, fsnotify v1.10.1, goccy/go-yaml v1.19.2)
- `.planning/research/PITFALLS.md` — 12 pitfalls including: modernc dialect must be `"sqlite3"` not `"sqlite"`, IsFromMe as FIRST gate, HistorySync flood on first pair (timestamp-filter), fsnotify watch parent dir not file, time.LoadLocation never time.Local

### Config
- `config.example.yaml` — canonical YAML shape (answers_cluster + matchers with cluster reference). **Structs MUST match this shape.**

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `main.go:12`: `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)` — signal-aware context already wired; new code plugs into `ctx.Done()` for graceful shutdown
- `config.go`: Empty structs `Config`, `DBConfig`, `MatcherConfig`, `AnswerConfig` and stub `loadConfig()` — these are the scaffolding to fill in

### Established Patterns
- `log/slog` already imported and used in `main.go` — continue using structured slog fields throughout
- `package main` currently flat — Phase 1 restructures to `cmd/bot/main.go` + `internal/`

### Integration Points
- whatsmeow `sqlstore.NewWithDB` needs a `*sql.DB` opened with `modernc.org/sqlite` driver registered as `"sqlite3"`
- fsnotify watcher targets the **parent directory** of the config file (atomic-rename editors replace the inode; file-level watch breaks silently)
- `atomic.Pointer[Snapshot]` is set in `internal/config` package; callers load snapshot for the full duration of one message-handling call

</code_context>

<specifics>
## Specific Ideas

- The `config.example.yaml` cluster pattern is the user's intended config shape — structs must match it exactly
- `scope:` / `limits:` / `log:` / `db:` section names came directly from the user (not inferred)
- `BOT_CONFIG` is the env var name for the config path override

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 1-Session & Config Foundations*
*Context gathered: 2026-05-23*
