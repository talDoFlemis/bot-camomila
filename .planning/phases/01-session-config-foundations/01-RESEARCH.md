# Phase 1: Session & Config Foundations — Research

**Researched:** 2026-05-23
**Domain:** Go whatsmeow session bootstrap, SQLite session store, YAML config with hot-reload, hexagonal package restructure
**Confidence:** HIGH (all stack packages verified via pkg.go.dev; architectural patterns verified via STACK.md/ARCHITECTURE.md/PITFALLS.md which were themselves sourced from official docs)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** `answers_cluster` pattern from `config.example.yaml`. Top-level `answers_cluster:` list with named pools; each matcher references a pool by name via `cluster:` key. Config loader resolves cluster → `[]string` answers at load time.
- **D-02:** Config struct sections (beyond `answers_cluster:` and `matchers:`):
  - `scope: { group_jid, owner_jids }` — group identity and owner allowlist
  - `limits: { quiet_hours: { start, end, timezone }, rate_cap: { per_min, per_hour } }` — behavioral limits
  - `log: { format }` — logging preferences
  - `db: { path }` — SQLite session DB path (inside YAML, not a CLI flag)
- **D-03:** CONFIG-02 validation runs at load time: invalid group JID, owner JID parse error, unresolvable timezone, matcher distance below min-word-length (distance 1 → ≥5 chars, distance 2 → ≥8 chars), self-loop guard (answer tokens must not fuzzy-match matcher keywords), and missing or ambiguous cluster reference.
- **D-04:** Config path: `--config` CLI flag (default `./config.yaml`) + `BOT_CONFIG` env var fallback. Flag wins over env.
- **D-05:** SQLite DB path: in the YAML under `db: { path: ./session.sqlite }`. Not a CLI flag.
- **D-06:** No other CLI flags in Phase 1.
- **D-07:** Record `startTime := time.Now()` at bot startup. In the message handler, drop any event whose `info.Timestamp.Before(startTime)` — silently discards all HistorySync-replayed messages without handling the HistorySync event type directly.

### Claude's Discretion

- **Package structure:** `cmd/bot/main.go` + `internal/` packages from day 1 (hexagonal as designed). `internal/whatsappadapter` is the ONLY package importing whatsmeow.
- **Log format selection:** TTY auto-detect via `isatty`. Text handler when stdout is a terminal (dev); JSON handler otherwise (Docker, CI). No `LOG_FORMAT` env var needed.

### Deferred Ideas (OUT OF SCOPE)

None — discussion stayed within phase scope.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SESSION-01 | Bot pairs to WhatsApp via on-terminal QR code on first launch | `client.GetQRChannel(ctx)` before `client.Connect()`; print `evt.Code` to stdout when `evt.Event == "code"` |
| SESSION-02 | Bot persists whatsmeow device session in SQLite (modernc.org/sqlite, CGO-free) and resumes without re-pairing | `sqlstore.NewWithDB(db, "sqlite3", log)` + `sql.Register("sqlite3", &sqlite.Driver{})` init pattern; `container.GetFirstDevice(ctx)` returns existing device |
| SESSION-03 | On startup, bot runs `PRAGMA integrity_check` and exits non-zero on failure | Run raw `db.QueryRow("PRAGMA integrity_check")` before handing db to sqlstore; check result == "ok" |
| SESSION-04 | On `events.LoggedOut`, bot logs at ERROR and exits non-zero | Type-switch on `*events.LoggedOut` in handler; `slog.Error(...); os.Exit(1)` or signal shutdown via context cancel |
| SESSION-05 | Bot survives transient disconnects (network blips, multi-device reconnect) without restart | Handle `*events.Disconnected` (log + wait for whatsmeow auto-reconnect); handle `*events.StreamReplaced` (log WARN; this is a permanent disconnect, same as LoggedOut) |
| CONFIG-01 | Load YAML config at path from CLI flag or env var (default `./config.yaml`) | `flag.String("config", "./config.yaml", ...)` + `BOT_CONFIG` env var; flag wins |
| CONFIG-02 | Validate at load time: invalid JID, bad TZ, distance/min-length, cluster ref, self-loop | `types.ParseJID` for JID validation; `time.LoadLocation` for TZ; custom validate() function; `agnivade/levenshtein` for self-loop check |
| CONFIG-03 | Expose config as immutable `atomic.Pointer[Snapshot]`; readers hold for full message-handling duration | `internal/config.Store` with `Get()`/`Swap()` backed by `atomic.Pointer[Snapshot]` |
| CONFIG-04 | Hot-reload via fsnotify watching parent directory, 200–500 ms debounce; on failure keep old snapshot | `w.Add(filepath.Dir(configPath))`; `time.AfterFunc(200ms, reload)`; swap only on valid parse+validate |
| CONFIG-05 | Fall back to 30–60 s mtime poll if fsnotify reports unrecoverable error | `time.NewTicker(30s)` + `os.Stat(configPath).ModTime()` comparison as belt-and-suspenders |
| SCOPE-01 | Listen to exactly one group JID; drop any message with different `Info.Chat` | First gate in adapter: `if evt.Info.Chat.String() != snap.Scope.GroupJID { return }` |
| SCOPE-02 | Drop messages with `Info.IsFromMe == true` immediately after group-JID gate | Second gate: `if evt.Info.IsFromMe { return }` |
| SCOPE-03 | Drop non-text message types before reaching matcher | Check `evt.Message.GetConversation() == "" && evt.Message.GetExtendedTextMessage() == nil` (and similar) |
| OBSERV-01 | All logs use `log/slog` with structured fields; JSON in Docker, text in dev | `isatty.IsTerminal(os.Stdout.Fd())` to select `NewTextHandler` vs `NewJSONHandler` |
| OBSERV-03 | Log whatsmeow lifecycle events (Connected, Disconnected, LoggedOut, PairSuccess) at INFO/ERROR | Type-switch handler cases for each lifecycle event type |
</phase_requirements>

---

## Summary

Phase 1 establishes the entire runtime foundation: a connected WhatsApp session, a validated YAML config system with hot-reload, the hexagonal package structure, and all lifecycle event handling. No matching or reply logic is included.

The two highest-risk areas are (1) the whatsmeow sqlstore dialect registration — `modernc.org/sqlite` registers as `"sqlite"` but sqlstore requires `"sqlite3"` dialect string, and (2) fsnotify parent-directory watching — most editors use atomic-rename saves that silently kill file-level watches. Both pitfalls are completely avoidable with the specific code patterns documented here.

The package restructure from flat `package main` to `cmd/bot/main.go` + `internal/` sub-packages must happen at the start of the phase. All subsequent tasks depend on the correct directory layout being in place.

**Primary recommendation:** Build in the strict order: (1) package scaffold, (2) `internal/config` load+validate+snapshot, (3) `internal/whatsappadapter` with sqlstore open + QR pair + event handler, (4) lifecycle logging, (5) fsnotify watcher. Validate QR pairing manually before adding any other logic — session persistence is a one-time-per-device concern and the riskiest integration step.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| YAML config load + validate | `internal/config` | `cmd/bot/main.go` (wires path) | Pure Go, no WhatsApp dependency; fully testable in isolation |
| Atomic config snapshot publish/read | `internal/config` | `internal/app` (wires store) | Single writer (watcher), many readers (event handler); `atomic.Pointer` fits exactly |
| fsnotify parent-dir watch + debounce | `internal/config` (watcher.go) | — | Lives with config since it writes the snapshot |
| mtime poll fallback | `internal/config` (watcher.go) | — | Co-located with fsnotify logic; falls back when fsnotify errors |
| CLI flag + env var parsing | `cmd/bot/main.go` | — | Entrypoint concern; not a domain concept |
| SQLite open + integrity_check | `internal/whatsappadapter` | — | The only package that touches the DB (via sqlstore) |
| whatsmeow sqlstore init + Upgrade | `internal/whatsappadapter` | — | Import firewall: whatsmeow lives only here |
| QR pairing flow | `internal/whatsappadapter` | — | Pairing is a whatsmeow-specific workflow |
| Event handler registration | `internal/whatsappadapter` | — | `client.AddEventHandler` is a whatsmeow API call |
| Lifecycle event logging (Connected/Disconnected/LoggedOut) | `internal/whatsappadapter` | — | Events are whatsmeow types; logging stays in the adapter |
| Group-JID gate + IsFromMe gate | `internal/whatsappadapter` (inbound.go) | — | Transport-level filter, not domain logic |
| HistorySync timestamp gate | `internal/whatsappadapter` (inbound.go) | — | Applied at event ingestion before any domain processing |
| Structured slog setup (TTY detect) | `cmd/bot/main.go` | — | One-time setup at process start; passed down via slog.SetDefault |
| Signal-aware context | `cmd/bot/main.go` | — | Already wired in existing scaffold |
| Graceful shutdown ordering | `cmd/bot/main.go` / `internal/app` | — | Disconnect → Wait → DB Close |

---

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.mau.fi/whatsmeow` | `v0.0.0-20260516102357-8d3700152a69` | WhatsApp multi-device client | Only maintained Go WhatsApp implementation; no CGO; no cloud API | [VERIFIED: pkg.go.dev] |
| `go.mau.fi/whatsmeow/store/sqlstore` | (sub-package, same module) | Device session persistence to SQLite | Required by whatsmeow; auto-upgrades schema; supports `"sqlite3"` dialect | [VERIFIED: pkg.go.dev] |
| `modernc.org/sqlite` | `v1.50.1` | CGO-free SQLite driver | Pure Go → static binary → distroless runtime; no gcc in build | [VERIFIED: pkg.go.dev] |
| `github.com/goccy/go-yaml` | `v1.19.2` | YAML config parsing | Actively maintained (monthly releases); `DisallowUnknownField()` for strict mode; no transitive deps | [VERIFIED: pkg.go.dev] |
| `github.com/fsnotify/fsnotify` | `v1.10.1` | Config file hot-reload | De-facto standard; `Event.Has(op)` bitmask API; documented atomic-rename caveat | [VERIFIED: pkg.go.dev] |
| `github.com/mattn/go-isatty` | `v0.0.22` | TTY detection for log format selection | Minimal, well-established; `IsTerminal(os.Stdout.Fd())` is the canonical pattern | [VERIFIED: pkg.go.dev] |
| `log/slog` | stdlib (Go 1.26.3) | Structured logging | Already imported in scaffold; JSON/Text handler selection via isatty | [VERIFIED: Go stdlib] |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `go.mau.fi/whatsmeow/util/log` (`waLog`) | bundled | Logger adapter interface | whatsmeow requires `waLog.Logger` (5-method interface), not `*slog.Logger`; implement a 30-line bridge |
| `go.mau.fi/whatsmeow/types` | bundled | JID type, `ParseJID`, server constants | Use `types.ParseJID` at config-load time to validate `group_jid` and `owner_jids`; `types.GroupServer == "g.us"` |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `modernc.org/sqlite` | `mattn/go-sqlite3` | CGO required; can't use `distroless/static`; slower builds; painful cross-arch — not worth it |
| `goccy/go-yaml` | `gopkg.in/yaml.v3` | yaml.v3 frozen since May 2022; goccy has stricter mode, monthly updates; for this project goccy is clearly better |
| `go-isatty` | `golang.org/x/term.IsTerminal` | `term.IsTerminal` is slightly lower-level; go-isatty is the conventional choice for this pattern |

**Installation:**
```bash
go get go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69
go get modernc.org/sqlite@v1.50.1
go get github.com/goccy/go-yaml@v1.19.2
go get github.com/fsnotify/fsnotify@v1.10.1
go get github.com/mattn/go-isatty@v0.0.22
go mod tidy
```

---

## Package Legitimacy Audit

> slopcheck failed to produce valid results for Go packages (it checked npm registry, not Go module proxy — cross-ecosystem confusion). All packages verified manually via pkg.go.dev.

| Package | Registry | Age | Source Repo | Manual Verification | Disposition |
|---------|----------|-----|-------------|---------------------|-------------|
| `go.mau.fi/whatsmeow` | Go module proxy | ~4 yrs (2022+) | github.com/tulir/whatsmeow | pkg.go.dev 200; STACK.md HIGH-confidence source | Approved |
| `modernc.org/sqlite` | Go module proxy | ~5 yrs | gitlab.com/cznic/sqlite | pkg.go.dev 200; v1.50.1 May 2026 | Approved |
| `github.com/goccy/go-yaml` | Go module proxy | ~5 yrs | github.com/goccy/go-yaml | pkg.go.dev 200; v1.19.2 Jan 2026; slopcheck [OK] | Approved |
| `github.com/fsnotify/fsnotify` | Go module proxy | ~10 yrs | github.com/fsnotify/fsnotify | pkg.go.dev 200; v1.10.1 May 2026 | Approved |
| `github.com/mattn/go-isatty` | Go module proxy | ~9 yrs | github.com/mattn/go-isatty | pkg.go.dev 200; v0.0.22 Apr 2026 | Approved |

**slopcheck note:** slopcheck v0.6.1 does not support Go module ecosystem and attempted npm registry checks, producing false `[SLOP]`/`[SUS]` verdicts for all Go packages. Results were discarded. All packages verified via pkg.go.dev (authoritative Go module registry) and pre-verified in STACK.md with HIGH confidence.

**Packages removed due to slopcheck [SLOP] verdict:** none (slopcheck results invalid for Go ecosystem)
**Packages flagged as suspicious [SUS]:** none after ecosystem-correct verification

---

## Architecture Patterns

### System Architecture Diagram

```
CLI args / BOT_CONFIG env
        │
        ▼
cmd/bot/main.go
  isatty.IsTerminal → select slog handler
  flag.Parse → configPath
  signal.NotifyContext → ctx
        │
        ▼
internal/app.Run(ctx, configPath)
        │
        ├──► internal/config.Load(path)
        │         yaml.Unmarshal + validate()
        │         → *Snapshot
        │
        ├──► internal/config.NewStore(snapshot)
        │         atomic.Pointer[Snapshot]
        │
        ├──► internal/config.NewWatcher(store, path)   ──► fsnotify.NewWatcher()
        │         w.Add(filepath.Dir(path))                  │
        │         debounce 200ms                             │ Events ch
        │         mtime poll 30s fallback                    │
        │         on change: Load+validate → store.Swap()    ◄─┘
        │
        └──► internal/whatsappadapter.New(cfgStore)
                  sql.Register("sqlite3", &sqlite.Driver{})
                  sql.Open("sqlite", dsn+"?_foreign_keys=on")
                  PRAGMA integrity_check → exit(1) on failure
                  sqlstore.NewWithDB(db, "sqlite3", waLog)
                  container.Upgrade(ctx)
                  container.GetFirstDevice(ctx)
                  whatsmeow.NewClient(device, waLog)
                  if client.Store.ID == nil → QR pair flow
                  client.AddEventHandler(adapter.onEvent)
                  client.Connect()
                        │
                        ▼ events
                  adapter.onEvent(evt interface{})
                    type switch:
                    *events.Connected    → slog.Info("connected")
                    *events.Disconnected → slog.Warn("disconnected; whatsmeow will reconnect")
                    *events.LoggedOut    → slog.Error("logged out"); os.Exit(1)
                    *events.PairSuccess  → slog.Info("paired", "jid", evt.ID)
                    *events.StreamReplaced → slog.Error("stream replaced"); os.Exit(1)
                    *events.Message      → gate pipeline (Phase 1: log only)
                      ├─ timestamp < startTime → drop (HistorySync flood)
                      ├─ evt.Info.Chat != groupJID → drop
                      ├─ evt.Info.IsFromMe → drop
                      ├─ not text message → drop
                      └─ slog.Info("message received", ...)
```

### Recommended Project Structure

```
bot-camomila/
├── cmd/
│   └── bot/
│       └── main.go              # entrypoint: slog setup, flag parse, signal ctx, app.Run
├── internal/
│   ├── app/
│   │   └── app.go               # composition root: wires config + adapter; Run(ctx, path)
│   ├── config/
│   │   ├── config.go            # struct types: Config, Snapshot, ScopeConfig, LimitsConfig, etc.
│   │   ├── load.go              # Load(path) → (*Snapshot, error); validate()
│   │   ├── store.go             # Store{atomic.Pointer[Snapshot]}; Get()/Swap()
│   │   └── watcher.go           # NewWatcher; Run(ctx); fsnotify + debounce + mtime fallback
│   ├── domain/
│   │   └── message.go           # domain.Message (pure struct; no whatsmeow imports)
│   └── whatsappadapter/
│       ├── client.go            # whatsmeow.Client lifecycle: Open, QR, Connect, Disconnect
│       ├── inbound.go           # onEvent dispatcher; gate logic; toDomain()
│       └── walog.go             # slogAdapter implementing waLog.Logger (5 methods)
├── config.example.yaml          # canonical YAML shape (keep as reference)
├── go.mod
└── go.sum
```

> Note: `internal/matcher/`, `internal/cooldown/`, `internal/killswitch/`, `internal/ownercommands/` are Phase 2+ packages. Create stubs or leave for later phases.

### Pattern 1: SQLite Driver Alias Registration

**What:** `modernc.org/sqlite` registers itself as driver name `"sqlite"`. whatsmeow's `sqlstore.NewWithDB` requires dialect string `"sqlite3"`. The fix is a one-time `sql.Register` alias call.

**When to use:** Always, in the init or startup path of `internal/whatsappadapter`.

**Example:**
```go
// internal/whatsappadapter/client.go
// Source: STACK.md (verified against pkg.go.dev/go.mau.fi/whatsmeow/store/sqlstore)
import (
    "database/sql"
    "go.mau.fi/whatsmeow/store/sqlstore"
    "modernc.org/sqlite"
    _ "modernc.org/sqlite" // side-effect: registers "sqlite" driver
)

func init() {
    // Register alias so sqlstore can use dialect "sqlite3"
    sql.Register("sqlite3", &sqlite.Driver{})
}

func openDB(path string) (*sql.DB, error) {
    dsn := "file:" + path + "?_foreign_keys=on"
    db, err := sql.Open("sqlite", dsn) // use the original driver name for sql.Open
    if err != nil {
        return nil, err
    }
    // integrity check before handing to sqlstore
    var result string
    if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
        db.Close()
        return nil, fmt.Errorf("SQLite integrity check failed: %v (result=%q)", err, result)
    }
    return db, nil
}

func openContainer(db *sql.DB, log waLog.Logger) (*sqlstore.Container, error) {
    container := sqlstore.NewWithDB(db, "sqlite3", log) // dialect = "sqlite3"
    if err := container.Upgrade(context.Background()); err != nil {
        return nil, err
    }
    return container, nil
}
```

### Pattern 2: QR Pairing Flow (First Run vs Resume)

**What:** On first launch `client.Store.ID == nil` → QR channel opened before Connect. On subsequent launches the store already has a device → just Connect.

**When to use:** Always check `client.Store.ID` before deciding whether to initiate QR flow.

**Example:**
```go
// internal/whatsappadapter/client.go
// Source: pkg.go.dev/go.mau.fi/whatsmeow (verified)
func (a *Adapter) Start(ctx context.Context) error {
    if a.client.Store.ID == nil {
        // First run — need to pair
        qrChan, err := a.client.GetQRChannel(ctx)
        if err != nil {
            return fmt.Errorf("get QR channel: %w", err)
        }
        go func() {
            for evt := range qrChan {
                if evt.Event == "code" {
                    // Print raw QR text; operator scans with phone
                    fmt.Println("Scan this QR code:")
                    fmt.Println(evt.Code)
                    // Optional: render with qrterminal for box-drawing QR
                }
            }
        }()
    }
    return a.client.Connect()
}
```

### Pattern 3: Event Handler with Gate Pipeline

**What:** Single `func(evt interface{})` registered via `client.AddEventHandler`. Type-switch dispatches to specific handlers. Phase 1 gates (group JID, IsFromMe, text-only, timestamp) live here.

**When to use:** This is the only event entry point; all inbound processing flows through it.

**Example:**
```go
// internal/whatsappadapter/inbound.go
// Source: pkg.go.dev/go.mau.fi/whatsmeow/types/events (verified)
func (a *Adapter) onEvent(evt interface{}) {
    switch v := evt.(type) {
    case *events.Connected:
        slog.Info("whatsapp connected")
    case *events.Disconnected:
        slog.Warn("whatsapp disconnected; auto-reconnect in progress")
    case *events.LoggedOut:
        slog.Error("whatsapp logged out", "on_connect", v.OnConnect, "reason", v.Reason)
        os.Exit(1) // SESSION-04: exit non-zero
    case *events.StreamReplaced:
        slog.Error("whatsapp stream replaced by another client; shutting down")
        os.Exit(1)
    case *events.PairSuccess:
        slog.Info("whatsapp paired", "jid", v.ID.String())
    case *events.Message:
        a.handleMessage(v)
    }
}

func (a *Adapter) handleMessage(evt *events.Message) {
    snap := a.cfg.Get() // one atomic load; immutable for this call

    // Gate 0: HistorySync flood filter (SESSION-02 / D-07)
    if evt.Info.Timestamp.Before(a.startTime) {
        return
    }
    // Gate 1: group JID (SCOPE-01)
    if evt.Info.Chat.String() != snap.Scope.GroupJID {
        return
    }
    // Gate 2: IsFromMe (SCOPE-02)
    if evt.Info.IsFromMe {
        return
    }
    // Gate 3: text only (SCOPE-03)
    text := extractText(evt.Message)
    if text == "" {
        return
    }

    slog.Info("message received",
        "group_jid", evt.Info.Chat.String(),
        "sender_jid", evt.Info.Sender.String(),
        "msg_id", evt.Info.ID,
    )
    // Phase 2+: pass domain.Message to matcher.Pipeline
}

func extractText(m *waE2E.Message) string {
    if m == nil {
        return ""
    }
    if t := m.GetConversation(); t != "" {
        return t
    }
    if ext := m.GetExtendedTextMessage(); ext != nil {
        return ext.GetText()
    }
    return ""
}
```

### Pattern 4: waLog.Logger Bridge to slog

**What:** whatsmeow requires a `waLog.Logger` (5-method interface). Bridge to stdlib `slog` with a 30-line adapter.

**Example:**
```go
// internal/whatsappadapter/walog.go
// Source: pkg.go.dev/go.mau.fi/whatsmeow/util/log (verified — interface has Debugf/Infof/Warnf/Errorf/Sub)
type slogAdapter struct {
    log *slog.Logger
}

func (a slogAdapter) Debugf(msg string, args ...interface{}) {
    a.log.Debug(fmt.Sprintf(msg, args...))
}
func (a slogAdapter) Infof(msg string, args ...interface{}) {
    a.log.Info(fmt.Sprintf(msg, args...))
}
func (a slogAdapter) Warnf(msg string, args ...interface{}) {
    a.log.Warn(fmt.Sprintf(msg, args...))
}
func (a slogAdapter) Errorf(msg string, args ...interface{}) {
    a.log.Error(fmt.Sprintf(msg, args...))
}
func (a slogAdapter) Sub(module string) waLog.Logger {
    return slogAdapter{log: a.log.With("module", module)}
}
```

### Pattern 5: Config Struct Matching config.example.yaml

**What:** Structs must exactly match the YAML shape in `config.example.yaml` plus the sections decided in D-02.

**Example:**
```go
// internal/config/config.go
// Source: config.example.yaml + CONTEXT.md D-01/D-02 (verified shape)
type Config struct {
    AnswersClusters []AnswersCluster `yaml:"answers_cluster"`
    Matchers        []MatcherConfig  `yaml:"matchers"`
    Scope           ScopeConfig      `yaml:"scope"`
    Limits          LimitsConfig     `yaml:"limits"`
    Log             LogConfig        `yaml:"log"`
    DB              DBConfig         `yaml:"db"`
}

type AnswersCluster struct {
    Name    string   `yaml:"name"`
    Answers []string `yaml:"answers"`
}

type MatcherConfig struct {
    Name     string   `yaml:"name"`
    Words    []string `yaml:"words"`
    Distance int      `yaml:"distance"`
    Cluster  string   `yaml:"cluster"` // references AnswersCluster.Name
}

type ScopeConfig struct {
    GroupJID  string   `yaml:"group_jid"`
    OwnerJIDs []string `yaml:"owner_jids"`
}

type LimitsConfig struct {
    QuietHours QuietHoursConfig `yaml:"quiet_hours"`
    RateCap    RateCapConfig    `yaml:"rate_cap"`
}

type QuietHoursConfig struct {
    Start    string `yaml:"start"`    // e.g. "22:00"
    End      string `yaml:"end"`      // e.g. "08:00"
    Timezone string `yaml:"timezone"` // IANA name e.g. "America/Sao_Paulo"
}

type RateCapConfig struct {
    PerMin  int `yaml:"per_min"`
    PerHour int `yaml:"per_hour"`
}

type LogConfig struct {
    Format string `yaml:"format"` // "json" | "text" | "" (auto-detect)
}

type DBConfig struct {
    Path string `yaml:"path"` // e.g. "./session.sqlite"
}

// Snapshot is the immutable, resolved form of Config (clusters already resolved into matchers).
type Snapshot struct {
    Scope    ScopeConfig
    Limits   LimitsConfig
    Log      LogConfig
    DB       DBConfig
    Matchers []ResolvedMatcher // Words + Answers already resolved
}

type ResolvedMatcher struct {
    Name     string
    Words    []string
    Distance int
    Answers  []string // resolved from cluster
}
```

### Pattern 6: fsnotify Parent-Directory Watch + Debounce

**What:** Watch parent directory, filter events by config filename, debounce 200ms. Fallback: mtime poll every 30s.

**Example:**
```go
// internal/config/watcher.go
// Source: ARCHITECTURE.md Pattern 2 + pkg.go.dev/github.com/fsnotify/fsnotify (verified)
func (w *Watcher) Run(ctx context.Context) error {
    fw, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }
    defer fw.Close()

    dir := filepath.Dir(w.configPath)
    base := filepath.Base(w.configPath)
    if err := fw.Add(dir); err != nil {
        return err
    }

    var debounce *time.Timer
    poll := time.NewTicker(30 * time.Second)
    defer poll.Stop()

    var lastMtime time.Time
    if fi, err := os.Stat(w.configPath); err == nil {
        lastMtime = fi.ModTime()
    }

    for {
        select {
        case <-ctx.Done():
            return nil

        case ev, ok := <-fw.Events:
            if !ok {
                // Channel closed (unrecoverable error) — fall through to poll
                goto pollOnly
            }
            if filepath.Base(ev.Name) != base {
                continue
            }
            if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Rename) {
                continue
            }
            if debounce != nil {
                debounce.Stop()
            }
            debounce = time.AfterFunc(200*time.Millisecond, w.reload)

        case fwErr, ok := <-fw.Errors:
            if !ok || fwErr != nil {
                slog.Warn("fsnotify error; switching to poll fallback", "err", fwErr)
                goto pollOnly
            }

        case <-poll.C:
            fi, err := os.Stat(w.configPath)
            if err == nil && fi.ModTime().After(lastMtime) {
                lastMtime = fi.ModTime()
                w.reload()
            }
        }
    }

pollOnly:
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

func (w *Watcher) reload() {
    snap, err := Load(w.configPath)
    if err != nil {
        slog.Warn("config reload failed; keeping previous config", "err", err)
        return
    }
    w.store.Swap(snap)
    slog.Info("config reloaded")
}
```

### Pattern 7: TTY-Based Log Handler Selection

**What:** Use `go-isatty` to select text vs JSON handler at startup. Must happen before any `slog` calls.

**Example:**
```go
// cmd/bot/main.go
// Source: pkg.go.dev/github.com/mattn/go-isatty (verified)
import (
    "github.com/mattn/go-isatty"
    "log/slog"
    "os"
)

func setupLogging() {
    var handler slog.Handler
    if isatty.IsTerminal(os.Stdout.Fd()) {
        handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
    } else {
        handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
    }
    slog.SetDefault(slog.New(handler))
}
```

### Anti-Patterns to Avoid

- **`sql.Open("sqlite3", ...)` without registering the alias first:** modernc registers as `"sqlite"`, not `"sqlite3"`. This produces a "unknown driver" panic at startup.
- **`sqlstore.New(ctx, "sqlite3", path, log)` vs `sqlstore.NewWithDB(db, "sqlite3", log)`:** Both are valid, but `New` calls `Upgrade` automatically; `NewWithDB` does NOT — you must call `container.Upgrade(ctx)` manually. The `NewWithDB` form is preferred because it lets you run `PRAGMA integrity_check` on the `*sql.DB` before passing it to sqlstore.
- **Checking `Info.IsFromMe` after group filter but before timestamp gate:** Timestamp gate must be FIRST (cheapest, eliminates entire HistorySync flood); then group JID; then IsFromMe.
- **Using `time.Local` anywhere:** Always `time.LoadLocation("IANA/TZ")`. The container's `time.Local` will be UTC.
- **Watching `config.yaml` file directly with `fsnotify.Add(configPath)`:** Atomic-rename editors silently kill file-level watches. Watch the directory.
- **Calling `waClient.Disconnect()` from inside an event handler:** This deadlocks. Disconnect must be called from outside the event handler goroutine (e.g., via context cancellation signal).
- **Mutating a Snapshot in place after publishing:** Snapshots must be immutable once stored. Any update replaces the whole pointer.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SQLite schema for session state | Custom tables for device keys, signal state | `sqlstore.Container.Upgrade(ctx)` | whatsmeow session schema is complex (Signal protocol state, 10+ tables); upstream maintains migrations |
| WhatsApp binary protocol parsing | Custom proto decode | `go.mau.fi/whatsmeow` client | Reverse-engineered protocol; impossible to maintain; upstream tracks WhatsApp updates |
| QR code terminal rendering | ASCII art QR generator | `github.com/mdp/qrterminal/v3` (optional) or just print `evt.Code` as-is | Operator can paste code into any QR generator; terminal rendering is a UX nicety not a requirement |
| fsnotify debounce | Custom ticker+channel combinator | `time.AfterFunc(200ms, reload)` pattern | Two lines; anything more complex introduces race conditions |
| YAML strict decoding | Custom unknown-field checker | `yaml.NewDecoder(r, yaml.DisallowUnknownField()).Decode(&v)` | goccy/go-yaml has this built-in |
| JID parsing and validation | Regex on JID strings | `types.ParseJID(s)` + `jid.Server == types.GroupServer` | JID format has edge cases (AD-JID, LID, legacy c.us); official parser handles all |

**Key insight:** The whatsmeow session store is the most complex "don't hand-roll" item. The sqlstore package manages schema versioning across whatsmeow version bumps — any custom DB layer on top of it will conflict with schema migrations.

---

## Common Pitfalls

### Pitfall 1: modernc dialect name mismatch

**What goes wrong:** `sqlstore.NewWithDB(db, "sqlite", log)` silently produces wrong SQL (dialect switch in sqlstore uses `"sqlite3"` as the expected string). Session writes fail or use wrong syntax.

**Why it happens:** `modernc.org/sqlite` registers as `"sqlite"`; sqlstore's dialect parameter is a separate concept from `database/sql`'s driver name.

**How to avoid:** Register alias: `sql.Register("sqlite3", &sqlite.Driver{})` in `init()`. Then use `sql.Open("sqlite", dsn)` for the raw DB and `sqlstore.NewWithDB(db, "sqlite3", log)` for the container.

**Warning signs:** Session tables not created; login loop at startup; `unknown dialect` errors.

### Pitfall 2: fsnotify file-level watch silently dies after first atomic-rename save

**What goes wrong:** After the operator edits config.yaml with vim or VS Code, the fsnotify event fires once, then never again. Config changes stop applying silently.

**Why it happens:** Editors replace the file inode via atomic rename (`write tmp → rename tmp → config.yaml`). The original inode being watched is gone.

**How to avoid:** `w.Add(filepath.Dir(configPath))` — watch the parent directory. Filter events by `filepath.Base(ev.Name) == "config.yaml"`.

**Warning signs:** "config reloaded" log appears for first edit, never for subsequent edits.

### Pitfall 3: HistorySync flood on first QR pair

**What goes wrong:** After pairing, whatsmeow replays weeks of group history as `*events.Message` events. Any gating or logging code fires for thousands of old messages. Phase 2 matchers would fire on historical messages.

**Why it happens:** WhatsApp's multi-device protocol syncs message history on first pair.

**How to avoid:** Record `startTime := time.Now()` at startup. First gate in `handleMessage`: `if evt.Info.Timestamp.Before(a.startTime) { return }`.

**Warning signs:** Thousands of "message received" log lines immediately after pairing; log timestamps are days in the past.

### Pitfall 4: `events.StreamReplaced` treated as transient disconnect

**What goes wrong:** Another client opens the same WhatsApp session. Bot sees `StreamReplaced`, treats it as a reconnectable `Disconnected` event, loops forever.

**Why it happens:** `StreamReplaced` is a permanent disconnect event (implements `PermanentDisconnect` interface). whatsmeow will not auto-reconnect.

**How to avoid:** Handle `*events.StreamReplaced` in the type switch with `os.Exit(1)` (or context cancel), same as `LoggedOut`.

**Warning signs:** Bot logs "disconnected" then immediately "connected" in a tight loop; CPU spikes; double session on WhatsApp.

### Pitfall 5: `PRAGMA integrity_check` skipped, bot starts on corrupt DB

**What goes wrong:** Corrupt SQLite file (from hard kill, mid-WAL-write) is silently opened. whatsmeow schema upgrade fails mid-way. Session is partially written. Pairing fails with cryptic errors.

**Why it happens:** Neither `sql.Open` nor `sqlstore.New` run integrity checks.

**How to avoid:** `db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)` before passing `db` to sqlstore. If `result != "ok"`, close DB and `os.Exit(1)`.

**Warning signs:** `malformed database schema` from sqlstore; `SQLITE_CORRUPT` errors.

### Pitfall 6: `waClient.Disconnect()` called from inside event handler goroutine

**What goes wrong:** Graceful shutdown code calls `Disconnect()` from within the handler (e.g., on `LoggedOut`). Deadlock — the handler blocks waiting for in-flight dispatch to complete, but `Disconnect` waits for handlers to return.

**Why it happens:** whatsmeow's event dispatcher holds a lock while calling handlers; `Disconnect()` tries to acquire the same lock.

**How to avoid:** On `LoggedOut`/`StreamReplaced`, cancel the context or use `os.Exit(1)` directly from the handler. The main goroutine (outside the handler) calls `Disconnect()`.

**Warning signs:** Bot hangs indefinitely after LoggedOut event; no further log output.

### Pitfall 7: Owner JID comparison without `.ToNonAD()`

**What goes wrong:** Owner JID in config is `5511999999999@s.whatsapp.net`. Incoming message sender JID is `5511999999999:5@s.whatsapp.net` (AD-JID with device suffix). String equality fails. Owner commands rejected.

**Why it happens:** WhatsApp multi-device uses AD-JIDs (with `:device` suffix) for the sender field. Config stores the base JID without device suffix.

**How to avoid:** `evt.Info.Sender.ToNonAD().String()` before comparing to owner JID list. Apply `.ToNonAD()` at config-load time too.

**Warning signs:** `!pause` DM from owner has no effect; no log line for owner command.

---

## Code Examples

### Open SQLite, integrity check, create sqlstore container

```go
// Source: pkg.go.dev/modernc.org/sqlite + pkg.go.dev/go.mau.fi/whatsmeow/store/sqlstore (verified)
import (
    "database/sql"
    "go.mau.fi/whatsmeow/store/sqlstore"
    "modernc.org/sqlite"
)

func init() {
    sql.Register("sqlite3", &sqlite.Driver{}) // alias for sqlstore dialect
}

func openStore(ctx context.Context, dbPath string, log waLog.Logger) (*sqlstore.Container, *sql.DB, error) {
    dsn := "file:" + dbPath + "?_foreign_keys=on"
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, nil, fmt.Errorf("open sqlite: %w", err)
    }
    var checkResult string
    if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&checkResult); err != nil {
        db.Close()
        return nil, nil, fmt.Errorf("integrity check query: %w", err)
    }
    if checkResult != "ok" {
        db.Close()
        return nil, nil, fmt.Errorf("SQLite integrity check failed: %q", checkResult)
    }
    container := sqlstore.NewWithDB(db, "sqlite3", log) // dialect = "sqlite3"
    if err := container.Upgrade(ctx); err != nil {
        db.Close()
        return nil, nil, fmt.Errorf("sqlstore upgrade: %w", err)
    }
    return container, db, nil
}
```

### Config load with goccy/go-yaml strict mode

```go
// Source: pkg.go.dev/github.com/goccy/go-yaml (verified)
import (
    "github.com/goccy/go-yaml"
    "os"
)

func Load(path string) (*Snapshot, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, fmt.Errorf("open config: %w", err)
    }
    defer f.Close()

    var cfg Config
    dec := yaml.NewDecoder(f, yaml.DisallowUnknownField())
    if err := dec.Decode(&cfg); err != nil {
        return nil, fmt.Errorf("parse yaml: %w", err)
    }
    return validate(cfg)
}
```

### JID validation at config load time

```go
// Source: pkg.go.dev/go.mau.fi/whatsmeow/types (verified)
import "go.mau.fi/whatsmeow/types"

func validateJID(raw string) (types.JID, error) {
    jid, err := types.ParseJID(raw)
    if err != nil {
        return types.JID{}, fmt.Errorf("invalid JID %q: %w", raw, err)
    }
    if jid.Server != types.GroupServer {
        return types.JID{}, fmt.Errorf("JID %q is not a group JID (server=%q, expected %q)", raw, jid.Server, types.GroupServer)
    }
    return jid, nil
}
```

### Atomic config store

```go
// Source: ARCHITECTURE.md Pattern 1 (verified against sync/atomic docs)
package config

import "sync/atomic"

type Store struct {
    ptr atomic.Pointer[Snapshot]
}

func NewStore(initial *Snapshot) *Store {
    s := &Store{}
    s.ptr.Store(initial)
    return s
}

func (s *Store) Get() *Snapshot  { return s.ptr.Load() }
func (s *Store) Swap(n *Snapshot) { s.ptr.Store(n) }
```

---

## State of the Art

| Old Approach | Current Approach | Notes |
|--------------|------------------|-------|
| `gopkg.in/yaml.v3` for config | `github.com/goccy/go-yaml` | yaml.v3 frozen May 2022; goccy active; for this project goccy is strictly better |
| `mattn/go-sqlite3` (CGO) | `modernc.org/sqlite` (pure Go) | CGO-free enables distroless runtime; no gcc in build pipeline |
| `logrus`/`zap` | `log/slog` (stdlib) | slog is stdlib since Go 1.21; whatsmeow community moving to it |
| File-level fsnotify watch | Parent-directory watch | File-level watch dies on atomic-rename; documented pitfall since fsnotify issue #17 |

**Deprecated/outdated:**
- `gopkg.in/yaml.v2`: EOL, superseded by v3. Do not use.
- `spf13/viper`: heavy; pulls cobra, etcd, consul, multiple config backends; overkill for one YAML file.
- Direct `slog.Default()` passed to whatsmeow: type mismatch; whatsmeow needs `waLog.Logger`.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `events.StreamReplaced` should trigger `os.Exit(1)` (treated same as LoggedOut) | Code Examples / Anti-Patterns | If whatsmeow auto-recovers from StreamReplaced in some cases, exit would be too aggressive — but docs say it implements `PermanentDisconnect` so this is low risk | [ASSUMED based on PITFALLS.md + docs description] |
| A2 | `os.Exit(1)` is the right response to `events.LoggedOut` from within the event handler (rather than context cancellation) | Code Examples | If graceful shutdown is needed (e.g., flush logs), `os.Exit` skips deferred closes; alternative is to signal ctx cancel and let main handle shutdown | [ASSUMED — context.cancel approach is safer but more complex] |
| A3 | `qrterminal/v3` is not required for Phase 1 — plain `fmt.Println(evt.Code)` is acceptable | Standard Stack | If operator cannot use a third-party QR renderer, Phase 1 pairing would require an extra package | [ASSUMED — requirements say "on-terminal QR code", which could mean rendered QR box] |

---

## Open Questions

1. **`os.Exit(1)` vs context cancellation for LoggedOut/StreamReplaced**
   - What we know: `events.LoggedOut` must cause non-zero exit and loud error logging (SESSION-04). `os.Exit(1)` from inside the event handler skips deferred `db.Close()` and `waClient.Disconnect()`.
   - What's unclear: Is clean shutdown (flushing WAL, closing DB) required, or is crash-fast acceptable for a "unrecoverable" event?
   - Recommendation: Use a `cancel()` call (from the `signal.NotifyContext`-derived cancel func, stored on the adapter) + return from the handler. Main goroutine handles the Disconnect → DB Close sequence. This avoids the deadlock pitfall and ensures clean WAL flush. `os.Exit(1)` is acceptable only if the planner explicitly decides clean shutdown is not needed for LoggedOut.

2. **QR code terminal rendering library**
   - What we know: SESSION-01 says "on-terminal QR code". `evt.Code` is a string that can be pasted into any QR tool.
   - What's unclear: Whether the operator expects a scannable box-drawing QR rendered inline in the terminal, or just a printable string.
   - Recommendation: Print the raw QR string + a message like "Scan this code or paste into a QR generator". Add `github.com/mdp/qrterminal/v3` in a follow-up if operator feedback requests it.

3. **`startTime` scope for HistorySync filter**
   - What we know: D-07 says record `startTime := time.Now()` at startup and drop events before it.
   - What's unclear: Whether `startTime` should be set in `main()` (before any setup) or in `adapter.Start()` (after connection established, before first event).
   - Recommendation: Set in `main()` (or `app.Run()` entry) before any whatsmeow operations. A few milliseconds earlier is safer than risk of a real message arriving before `startTime` is recorded.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | All compilation | Yes | 1.26.3 | — |
| git | Module downloads | Yes | (git present) | — |
| Internet access | `go get` of new deps | Assumed | — | Vendor deps if offline |
| SQLite (runtime) | Session store | Built-in (modernc is pure Go) | SQLite 3.53.1 embedded | — |
| WhatsApp account + phone for QR | SESSION-01 pairing | Manual prerequisite | — | Cannot automate; operator must provide |

**Missing dependencies with no fallback:**
- A physical WhatsApp account and phone for QR pairing — this is a manual prerequisite that no code can substitute.

**Missing dependencies with fallback:**
- None.

---

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | Yes (WhatsApp session) | whatsmeow QR pairing + session persistence in SQLite; no custom auth |
| V3 Session Management | Yes | `sqlstore` owns session state; `PRAGMA integrity_check` on startup; single-process lockfile recommended |
| V4 Access Control | Yes (scope gates) | Group JID allow-list; IsFromMe filter; owner JID normalization via `.ToNonAD()` |
| V5 Input Validation | Yes (config) | `types.ParseJID`; `time.LoadLocation`; distance min-length; goccy strict YAML decode |
| V6 Cryptography | No (Phase 1) | whatsmeow handles Signal protocol encryption internally; no app-level crypto in Phase 1 |

### Known Threat Patterns for this Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Session DB file exposure | Information Disclosure | Volume permissions 0700; `.dockerignore` excludes `*.sqlite*`; never commit DB |
| Scope leak (wrong JID) | Elevation of Privilege | Allow-list gate as first check; `types.ParseJID` validates format |
| Self-reply loop | Denial of Service | `IsFromMe` gate as second check; never route bot's own messages to handler |
| Config symlink swap | Tampering | Resolve config path to absolute with `filepath.Abs`; log resolved path on every reload |
| HistorySync replay of old messages | Denial of Service | `startTime` timestamp gate eliminates entire flood in one comparison |
| SQLite corruption on hard kill | Denial of Service | `PRAGMA integrity_check` on startup; graceful SIGTERM handler; single-process invariant |

---

## Sources

### Primary (HIGH confidence)

- `pkg.go.dev/go.mau.fi/whatsmeow` — Client API, GetQRChannel, Connect, AddEventHandler signatures [VERIFIED: pkg.go.dev]
- `pkg.go.dev/go.mau.fi/whatsmeow/store/sqlstore` — NewWithDB, New, Upgrade, GetFirstDevice signatures; dialect string `"sqlite3"` [VERIFIED: pkg.go.dev]
- `pkg.go.dev/go.mau.fi/whatsmeow/types/events` — Connected, Disconnected, LoggedOut, StreamReplaced, PairSuccess, Message struct fields [VERIFIED: pkg.go.dev]
- `pkg.go.dev/go.mau.fi/whatsmeow/types#MessageInfo` — MessageSource embedded fields: Chat, Sender, IsFromMe, IsGroup [VERIFIED: pkg.go.dev]
- `pkg.go.dev/go.mau.fi/whatsmeow/types#JID` — JID struct, ToNonAD(), server constants (GroupServer = "g.us") [VERIFIED: pkg.go.dev]
- `pkg.go.dev/go.mau.fi/whatsmeow/util/log` — waLog.Logger interface: Debugf/Infof/Warnf/Errorf/Sub [VERIFIED: pkg.go.dev]
- `pkg.go.dev/modernc.org/sqlite` — v1.50.1 driver name `"sqlite"`; pure Go; alias registration pattern [VERIFIED: pkg.go.dev]
- `pkg.go.dev/github.com/goccy/go-yaml` — v1.19.2; `DisallowUnknownField()` decoder option [VERIFIED: pkg.go.dev]
- `pkg.go.dev/github.com/fsnotify/fsnotify` — v1.10.1; Watcher.Add, Events channel, Op flags (Write/Create/Rename/Remove/Chmod), Event.Has() [VERIFIED: pkg.go.dev]
- `pkg.go.dev/github.com/mattn/go-isatty` — v0.0.22; `IsTerminal(fd uintptr) bool` [VERIFIED: pkg.go.dev]
- `.planning/research/STACK.md` — pinned versions, whatsmeow gotchas, modernc dialect alias [CITED: project research]
- `.planning/research/ARCHITECTURE.md` — package layout, atomic snapshot pattern, fsnotify debounce pattern, startup sequence [CITED: project research]
- `.planning/research/PITFALLS.md` — 12 pitfalls including dialect, IsFromMe ordering, HistorySync flood, corruption [CITED: project research]

### Secondary (MEDIUM confidence)

- `CLAUDE.md` critical invariants — confirms all decisions above are consistent with project constraints [CITED: project file]
- `config.example.yaml` — canonical YAML shape that structs must match [CITED: project file]

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all packages verified via pkg.go.dev with exact versions
- Architecture: HIGH — patterns from ARCHITECTURE.md which was sourced from official whatsmeow/fsnotify/Go docs
- Code patterns: HIGH — API signatures verified directly from pkg.go.dev for each library
- Pitfalls: HIGH — sourced from PITFALLS.md (itself sourced from official whatsmeow GitHub issues + fsnotify upstream)
- Security: MEDIUM — ASVS mapping is [ASSUMED] as applied to this specific stack

**Research date:** 2026-05-23
**Valid until:** 2026-06-23 (whatsmeow pseudo-version may change; check for protocol updates monthly)
