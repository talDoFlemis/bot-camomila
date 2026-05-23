# bot-camomila — Claude Guidance

## Project

WhatsApp de-escalation bot for one group chat. Fuzzy-matches keywords (Levenshtein distance) in messages and quoted text, replies with a randomly-picked calming answer threaded to the triggering message. Named for chamomile tea.

Planning artifacts: `.gsd/SPEC.md`, `.gsd/REQUIREMENTS.md`, `.gsd/ROADMAP.md`.

## Tech Stack

- Go 1.26.3, module `github.com/taldoflemis/bot-camomila`
- `go.mau.fi/whatsmeow` — WhatsApp multi-device client
- `modernc.org/sqlite` — CGO-free SQLite (register dialect as `"sqlite3"` for sqlstore)
- `agnivade/levenshtein` — rune-safe Levenshtein
- `fsnotify/fsnotify` — config hot-reload (watch **parent directory**, not the file)
- `goccy/go-yaml` — YAML parsing
- `log/slog` — structured logging (JSON in Docker, text in dev)

## Architecture

Hexagonal layout — `internal/whatsappadapter` is the **only** package that imports whatsmeow. Everything else is pure Go, testable without a WhatsApp connection.

```
cmd/bot/main.go
internal/
  config/         # YAML load, validate, atomic.Pointer[Snapshot], fsnotify hot-reload
  domain/         # pure types: Message, Matcher, MatchResult
  matcher/        # Levenshtein fuzzy match, NFC normalize, per-token, min-length guard
  cooldown/       # sync.Map + time.Since (monotonic), background reaper
  killswitch/     # atomic.Bool — lives outside config snapshot
  whatsappadapter/# ONLY whatsmeow import; QR pair, event fan-out, send reply
  ownercommands/  # !pause / !resume DM command handler
```

## Critical Invariants

- **IsFromMe filter first** — drop `Info.IsFromMe == true` immediately after group-JID gate (self-reply loop prevention).
- **Match+reply+cooldown ship as one unit** — deploying reply without cooldowns active in a live group = spam footgun.
- **Kill switch outside config snapshot** — `atomic.Bool` in a `Runtime` struct; hot-reload must never reset it.
- **fsnotify watches parent directory** — atomic-rename editors (vim, most editors) replace the file inode; file-level watch breaks silently.
- **Debounce 200–500 ms** on fsnotify events before re-parsing config.
- **modernc.org/sqlite dialect = `"sqlite3"`** — not `"sqlite"`. Mismatch silently breaks session writes.
- **time.LoadLocation always** — never `time.Local`. Docker image bundles tzdata.
- **time.Since for cooldowns** — not `time.Unix()` round-trips; monotonic clock avoids drift.
- **JID normalization** — call `.ToNonAD()` when comparing sender JID to owner allowlist.
- **HistorySync flood** — on first pair, whatsmeow replays old messages; timestamp-filter events to drop any message predating bot start time.
- **Distance min-length** — distance 1 → words ≥5 chars; distance 2 → words ≥8 chars. Enforce at config load AND at match time.

## Config Hot-Reload Pattern

```go
// Reader: hold snapshot for full duration of one message-handling call
cfg := cfgPtr.Load()
// Writer: validate first, then atomic swap; on failure keep old snapshot + log WARN
if err := validate(next); err != nil { slog.Warn(...); return }
cfgPtr.Store(next)
```

## Logging

Every match decision must be logged with `reason` field:
- `matched` — matcher fired, reply sent
- `cooldown_matcher` — per-matcher cooldown active
- `cooldown_user` — per-user cooldown active
- `quiet_hours` — quiet hours window
- `rate_cap` — global rate cap exceeded
- `kill_switch` — bot paused

## Docker

Multi-stage: `golang:1.26.3-alpine` builder → `gcr.io/distroless/static-debian13:nonroot` runtime.
Build flags: `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`. Target image ≤25 MB.
distroless/static-debian13:nonroot includes tzdata, ca-certs, nonroot uid 65532.

## GSD Workflow (Antigravity)

- Framework: GSD for Antigravity (`.gsd/` directory)
- Mode: YOLO
- Granularity: coarse (4 phases)
- Git tracking: yes

Current phase: 2 (Matcher Pipeline & Safe Dispatch). Run `/plan 2` to start planning.
