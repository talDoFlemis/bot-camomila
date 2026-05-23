# Walking Skeleton — bot-camomila

**Phase:** 1 — Session & Config Foundations
**Generated:** 2026-05-23

## Capability Proven End-to-End

> Operator runs `./bot --config config.yaml`, scans QR code on phone, sends a text message to the configured WhatsApp group, and sees a structured log line with `event=message_received`, `group_jid`, `sender_jid`, and `msg_id`. Stopping the bot with SIGTERM exits cleanly (exit code 0) within a few seconds.

## Architectural Decisions

| Decision | Choice | Rationale |
|---|---|---|
| WhatsApp client library | `go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69` | Only maintained CGO-free Go WhatsApp multi-device implementation; tracks protocol updates |
| SQLite driver | `modernc.org/sqlite@v1.50.1` | Pure Go (CGO-free) — enables `distroless/static` runtime in Phase 4; no gcc in build pipeline |
| SQLite dialect alias | `sql.Register("sqlite3", &sqlite.Driver{})` in `init()` | whatsmeow sqlstore requires dialect `"sqlite3"`; modernc registers as `"sqlite"` — alias bridges the mismatch |
| Session store | `sqlstore.NewWithDB(db, "sqlite3", log)` + manual `container.Upgrade(ctx)` | NewWithDB form chosen over New() to allow PRAGMA integrity_check before handing DB to sqlstore |
| Config format | YAML via `github.com/goccy/go-yaml@v1.19.2` with `DisallowUnknownField()` | Strict mode rejects typos; actively maintained (yaml.v3 frozen May 2022); no transitive deps |
| Config hot-reload | `fsnotify` watching **parent directory** with 200ms debounce | File-level watch breaks on atomic-rename saves (vim, VS Code); parent-dir watch survives inode replacement |
| Hot-reload fallback | 30s mtime poll | Belt-and-suspenders when fsnotify reports unrecoverable error |
| Config concurrency | `atomic.Pointer[Snapshot]` — single writer (watcher), many readers (event handler) | Lock-free reads; readers hold pointer for full message-handling call duration |
| Log format | TTY auto-detect via `go-isatty`: text handler in dev, JSON handler in Docker/CI | Zero config; correct behavior in all environments without env vars |
| Structured logging | `log/slog` stdlib | Stdlib since Go 1.21; JSON/Text handler selection; whatsmeow community moving away from logrus/zap |
| waLog bridge | Custom 5-method `slogAdapter` in `internal/whatsappadapter/walog.go` | whatsmeow requires `waLog.Logger` (not `*slog.Logger`); 30-line adapter bridges them |
| Package layout | Hexagonal: `cmd/bot/main.go` + `internal/{config,domain,app,whatsappadapter}` | `internal/whatsappadapter` is the sole whatsmeow import boundary; all other packages are pure Go and testable without a phone |
| Config path | `--config` CLI flag (default `./config.yaml`) + `BOT_CONFIG` env var fallback (flag wins) | D-04: no other CLI flags in Phase 1 |
| DB path | `db.path` in YAML config (default `./session.sqlite`) | D-05: not a CLI flag — co-located with other runtime config |
| HistorySync flood filter | `startTime := time.Now()` recorded in `New()` before Connect; gate 0 drops `evt.Info.Timestamp.Before(startTime)` | D-07: eliminates entire WhatsApp history replay on first pair without handling HistorySync event type |
| LoggedOut handling | `a.cancel()` in event handler → graceful shutdown in main goroutine | Avoids Disconnect()-in-handler deadlock; ensures clean WAL flush before exit |
| StreamReplaced handling | Same as LoggedOut (`a.cancel()`) — permanent disconnect | `events.StreamReplaced` implements `PermanentDisconnect`; whatsmeow will not auto-reconnect |
| Disconnected handling | Log WARN + return (no cancel) | whatsmeow auto-reconnects transient disconnects (SESSION-05) |
| Self-loop guard | CONFIG-02 check at load time: answer tokens must not fuzzy-match own matcher keywords | Prevents bot from triggering itself on its own replies when Phase 2 is active |

## Stack Touched in Phase 1

- [x] Signal-aware context loop (`cmd/bot/main.go` — `signal.NotifyContext` already in scaffold)
- [x] YAML config load + validate (`internal/config/load.go` — strict decode, JID/TZ/distance/cluster/self-loop checks)
- [x] Atomic config snapshot (`internal/config/store.go` — `atomic.Pointer[Snapshot]`)
- [x] fsnotify hot-reload (`internal/config/watcher.go` — parent-dir watch, 200ms debounce, 30s mtime fallback)
- [x] SQLite session store (`internal/whatsappadapter/client.go` — integrity_check + sqlstore)
- [x] QR pairing flow (`internal/whatsappadapter/client.go` — `GetQRChannel`, print code to stdout)
- [x] Group JID gate (`internal/whatsappadapter/inbound.go` — SCOPE-01)
- [x] IsFromMe gate (`internal/whatsappadapter/inbound.go` — SCOPE-02)
- [x] Text-only gate (`internal/whatsappadapter/inbound.go` — SCOPE-03)
- [x] HistorySync timestamp gate (`internal/whatsappadapter/inbound.go` — D-07)
- [x] Lifecycle event logging (`internal/whatsappadapter/inbound.go` — OBSERV-03)
- [x] Structured slog setup (`cmd/bot/main.go` — TTY auto-detect, OBSERV-01)
- [x] Graceful shutdown (`internal/app/app.go` — ctx.Done → adapter.Disconnect → db.Close)
- [x] waLog adapter (`internal/whatsappadapter/walog.go` — 5-method slogAdapter)
- [x] Domain type (`internal/domain/message.go` — pure Message struct, no whatsmeow imports)

## Out of Scope (Deferred to Later Slices)

- Matcher pipeline, fuzzy-match logic, reply dispatch (Phase 2)
- Cooldown state (`sync.Map`), quiet-hours enforcement, rate cap (Phase 2)
- Reaction-as-reply mode (v2 deferred)
- Owner kill switch commands (`!pause` / `!resume`) (Phase 3)
- Multi-device JID normalization for owner commands (Phase 3)
- Docker packaging, distroless image, docker-compose (Phase 4)
- `PRAGMA integrity_check` exit-code test automation (manual verification in Phase 1 checkpoint)
- `/healthz` HTTP endpoint (v2 deferred)
- Persisted cooldown state across restarts (v2 deferred)

## Subsequent Slice Plan

- **Phase 2: Matcher Pipeline & Safe Dispatch** — Bot detects trigger words via Levenshtein distance and replies with a randomly-picked calming answer; cooldowns, quiet hours, and global rate cap ship as one indivisible bundle
- **Phase 3: Owner Commands & Operability** — Operator can `!pause` / `!resume` via DM; kill switch is an `atomic.Bool` in a `Runtime` struct outside the config snapshot
- **Phase 4: Docker Packaging & Deploy** — Multi-stage build: `golang:1.26.3-alpine` builder → `gcr.io/distroless/static-debian13:nonroot` runtime; final image ≤25 MB; `docker-compose.yml` with bind-mount volumes
