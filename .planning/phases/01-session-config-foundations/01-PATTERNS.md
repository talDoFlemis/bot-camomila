# Phase 1: Session & Config Foundations - Pattern Map

**Mapped:** 2026-05-23
**Files analyzed:** 10 new files to create
**Analogs found:** 2 / 10 (codebase is near-empty scaffold; RESEARCH.md patterns fill the gap)

---

## File Classification

| New / Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/bot/main.go` | entrypoint | request-response | `main.go` (scaffold) | role-match (same role, must be moved + extended) |
| `internal/app/app.go` | composition-root | request-response | `main.go` (scaffold, wiring portion) | partial (wiring idiom only) |
| `internal/config/config.go` | model | transform | `config.go` (scaffold stubs) | role-match (same structs, fill in) |
| `internal/config/load.go` | service | transform | `config.go` stub `loadConfig()` | role-match (stub → real implementation) |
| `internal/config/store.go` | utility | event-driven | none | no analog |
| `internal/config/watcher.go` | service | event-driven | none | no analog |
| `internal/domain/message.go` | model | transform | none | no analog |
| `internal/whatsappadapter/client.go` | service | request-response | none | no analog |
| `internal/whatsappadapter/inbound.go` | controller | event-driven | none | no analog |
| `internal/whatsappadapter/walog.go` | utility | request-response | none | no analog |

---

## Pattern Assignments

### `cmd/bot/main.go` (entrypoint, request-response)

**Analog:** `main.go` (lines 1-18) — the existing scaffold is the direct source; this file replaces it after the package restructure.

**Imports pattern** (`main.go` lines 1-9):
```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
)
```
New file adds `flag`, `github.com/mattn/go-isatty`, and the internal `app` package.

**Signal-aware context pattern** (`main.go` lines 12-17):
```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
defer stop()
<-ctx.Done()
```
Copy this block verbatim. The new entrypoint wraps it: set up logging first, parse flags, call `app.Run(ctx, configPath)`, then block on `<-ctx.Done()`.

**Slog setup pattern** (from RESEARCH.md Pattern 7, no codebase analog exists yet):
```go
// Must execute before any slog call — select handler based on TTY
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

**Flag + env-var pattern** (RESEARCH.md CONFIG-01, CONTEXT.md D-04):
```go
// Flag wins over env var
configPath := flag.String("config", "./config.yaml", "path to config file")
flag.Parse()
if *configPath == "./config.yaml" {
    if v := os.Getenv("BOT_CONFIG"); v != "" {
        *configPath = v
    }
}
```

**slog first call** (`main.go` line 14):
```go
slog.Info("Starting application...")
```
Match the style: `slog.Info` / `slog.Error` with a plain message string and structured key-value pairs as trailing args (`"key", value`). Never use `fmt.Sprintf` inside slog calls.

---

### `internal/app/app.go` (composition-root, request-response)

**Analog:** `main.go` wiring portion (lines 11-17) — the `defer stop()` / `<-ctx.Done()` idiom is reused; wiring of sub-packages has no existing analog.

**Core wiring pattern** (derived from `main.go` shutdown idiom + RESEARCH.md architecture diagram):
```go
func Run(ctx context.Context, configPath string) error {
    // 1. Load initial config
    // 2. Create config.Store
    // 3. Start config.Watcher (goroutine)
    // 4. Create whatsappadapter.Adapter
    // 5. Start adapter (QR / Connect)
    // 6. Block on ctx.Done()
    // 7. Graceful shutdown: adapter.Disconnect() → db.Close()
    <-ctx.Done()
    return nil
}
```
Shutdown order is: `adapter.Disconnect()` then `db.Close()`. Never call `Disconnect()` from inside an event handler (deadlock — see RESEARCH.md Pitfall 6).

**Slog usage** — every lifecycle log uses structured fields:
```go
slog.Info("starting bot", "config_path", configPath)
slog.Info("bot stopped")
slog.Error("config load failed", "err", err, "path", configPath)
```

---

### `internal/config/config.go` (model, transform)

**Analog:** `config.go` (lines 1-13) — same file role; the empty structs are the starting point.

**Existing stub** (`config.go` lines 1-13):
```go
package main

type Config struct{}
type DBConfig struct{}
type MatcherConfig struct{}
type AnswerConfig struct{}

func loadConfig() (*Config, error) {
    return nil, nil
}
```
Replace `package main` with `package config`. Expand each struct. Move `loadConfig` to `load.go`.

**Struct shape** must match `config.example.yaml` exactly (CONTEXT.md D-01, D-02). Canonical YAML (`config.example.yaml` lines 1-12):
```yaml
answers_cluster:
  - name: sefaz_cluster
    answers:
      - Olá {REPLIED_USER} não podemos falar essa palavra aqui tá?
matchers:
  - name: sefaz
    words:
      - SEFAZ
    distance: 1
    cluster: sefaz_cluster
```

**Full struct pattern** (RESEARCH.md Pattern 5):
```go
package config

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
    Start    string `yaml:"start"`    // "22:00"
    End      string `yaml:"end"`      // "08:00"
    Timezone string `yaml:"timezone"` // IANA name: "America/Sao_Paulo"
}

type RateCapConfig struct {
    PerMin  int `yaml:"per_min"`
    PerHour int `yaml:"per_hour"`
}

type LogConfig struct {
    Format string `yaml:"format"` // unused in Phase 1 (isatty auto-detect wins)
}

type DBConfig struct {
    Path string `yaml:"path"` // "./session.sqlite"
}

// Snapshot is the immutable resolved form. Callers hold this for
// the full duration of one message-handling call.
type Snapshot struct {
    Scope    ScopeConfig
    Limits   LimitsConfig
    Log      LogConfig
    DB       DBConfig
    Matchers []ResolvedMatcher
}

type ResolvedMatcher struct {
    Name     string
    Words    []string
    Distance int
    Answers  []string // resolved from cluster at load time
}
```

---

### `internal/config/load.go` (service, transform)

**Analog:** `config.go` stub `loadConfig()` (line 11-13) — same function responsibility; implementation derives from RESEARCH.md.

**Existing stub** (`config.go` line 11):
```go
func loadConfig() (*Config, error) { return nil, nil }
```

**YAML strict-decode pattern** (RESEARCH.md Code Examples):
```go
package config

import (
    "fmt"
    "os"

    "github.com/goccy/go-yaml"
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

**Validation pattern** (CONTEXT.md D-03, RESEARCH.md CONFIG-02):
```go
func validate(cfg Config) (*Snapshot, error) {
    // 1. Validate group JID
    // 2. Validate each owner JID
    // 3. Validate timezone via time.LoadLocation (never time.Local)
    // 4. Validate distance min-length: distance 1 → ≥5 chars, distance 2 → ≥8 chars
    // 5. Resolve cluster refs; error on missing or ambiguous
    // 6. Self-loop guard: answer tokens must not fuzzy-match matcher keywords
    // Return *Snapshot with resolved matchers
}
```

**JID validation pattern** (RESEARCH.md Code Examples):
```go
import "go.mau.fi/whatsmeow/types"

func validateGroupJID(raw string) (types.JID, error) {
    jid, err := types.ParseJID(raw)
    if err != nil {
        return types.JID{}, fmt.Errorf("invalid JID %q: %w", raw, err)
    }
    if jid.Server != types.GroupServer {
        return types.JID{}, fmt.Errorf("JID %q is not a group JID (server=%q, expected %q)",
            raw, jid.Server, types.GroupServer)
    }
    return jid, nil
}
```

**Error wrapping style** — always `fmt.Errorf("context: %w", err)`. Match the pattern from the existing `loadConfig` signature: return `(*T, error)`, never panic.

---

### `internal/config/store.go` (utility, event-driven)

**Analog:** none in codebase. Pattern sourced entirely from RESEARCH.md.

**Atomic store pattern** (RESEARCH.md Code Examples + CLAUDE.md hot-reload pattern):
```go
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

// Get returns the current snapshot. Callers hold the returned pointer
// for the full duration of one message-handling call — do NOT call Get
// repeatedly within one call.
func (s *Store) Get() *Snapshot  { return s.ptr.Load() }

// Swap atomically replaces the snapshot. Only the watcher goroutine calls this.
func (s *Store) Swap(n *Snapshot) { s.ptr.Store(n) }
```

**CLAUDE.md invariant to enforce:**
```go
// Reader pattern (inside event handler):
cfg := cfgPtr.Load()     // one load; use cfg for full call duration

// Writer pattern (inside watcher reload):
if err := validate(next); err != nil {
    slog.Warn("config reload failed; keeping previous config", "err", err)
    return
}
cfgPtr.Store(next)
slog.Info("config reloaded")
```

---

### `internal/config/watcher.go` (service, event-driven)

**Analog:** none in codebase. Pattern sourced entirely from RESEARCH.md.

**fsnotify parent-dir watch + debounce pattern** (RESEARCH.md Pattern 6, CLAUDE.md critical invariants):

Key invariants to encode:
- `fw.Add(filepath.Dir(configPath))` — NEVER `fw.Add(configPath)` (atomic-rename editors break file-level watch)
- Filter events: `filepath.Base(ev.Name) == base`
- Debounce: `time.AfterFunc(200*time.Millisecond, w.reload)`
- Fallback ticker: `time.NewTicker(30 * time.Second)` + mtime comparison
- On `fw.Errors` channel closed or error: fall through to poll-only loop

```go
package config

import (
    "log/slog"
    "os"
    "path/filepath"
    "time"

    "github.com/fsnotify/fsnotify"
)

type Watcher struct {
    store      *Store
    configPath string
}

func NewWatcher(store *Store, configPath string) *Watcher {
    return &Watcher{store: store, configPath: configPath}
}

func (w *Watcher) Run(ctx context.Context) error {
    fw, err := fsnotify.NewWatcher()
    // ... (see RESEARCH.md Pattern 6 for full implementation)
    dir := filepath.Dir(w.configPath)   // watch PARENT DIRECTORY
    base := filepath.Base(w.configPath)
    fw.Add(dir)
    // debounce via time.AfterFunc(200ms, w.reload)
    // mtime poll fallback via time.NewTicker(30s)
}

func (w *Watcher) reload() {
    snap, err := Load(w.configPath)
    if err != nil {
        slog.Warn("config reload failed; keeping previous config", "err", err)
        return
    }
    w.store.Swap(snap)
    slog.Info("config reloaded", "path", w.configPath)
}
```

**Slog usage inside watcher** — `slog.Warn` on reload failure (never fatal; keep old config), `slog.Info` on success, `slog.Warn` on fsnotify error before switching to poll.

---

### `internal/domain/message.go` (model, transform)

**Analog:** none in codebase. Pure Go struct; no whatsmeow imports allowed.

**Pattern:** Minimal pure struct. No methods that touch whatsmeow types. The adapter's `inbound.go` constructs this from `*events.Message` via a `toDomain()` function.

```go
package domain

import "time"

// Message is a transport-agnostic representation of an inbound group message.
// No whatsmeow types appear here — hexagonal boundary enforced.
type Message struct {
    ID        string
    GroupJID  string
    SenderJID string // ToNonAD() form — no device suffix
    Text      string
    Timestamp time.Time
}
```

**Module path** (`go.mod` line 1): `github.com/taldoflemis/bot-camomila` — all internal import paths are `github.com/taldoflemis/bot-camomila/internal/...`.

---

### `internal/whatsappadapter/client.go` (service, request-response)

**Analog:** none in codebase. Pattern sourced entirely from RESEARCH.md.

**SQLite dialect alias — CRITICAL** (RESEARCH.md Pattern 1, Pitfall 1, CLAUDE.md critical invariants):
```go
package whatsappadapter

import (
    "database/sql"

    "modernc.org/sqlite"
)

func init() {
    // modernc registers as "sqlite"; sqlstore needs dialect "sqlite3"
    sql.Register("sqlite3", &sqlite.Driver{})
}
```
The `init()` pattern ensures the alias is registered before any `sql.Open` call regardless of call order.

**SQLite open + integrity check + sqlstore pattern** (RESEARCH.md Code Examples):
```go
func openStore(ctx context.Context, dbPath string, log waLog.Logger) (*sqlstore.Container, *sql.DB, error) {
    dsn := "file:" + dbPath + "?_foreign_keys=on"
    db, err := sql.Open("sqlite", dsn)  // driver name = "sqlite"
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
    container := sqlstore.NewWithDB(db, "sqlite3", log)  // dialect = "sqlite3"
    if err := container.Upgrade(ctx); err != nil {       // NewWithDB does NOT auto-upgrade
        db.Close()
        return nil, nil, fmt.Errorf("sqlstore upgrade: %w", err)
    }
    return container, db, nil
}
```

**QR pairing vs resume pattern** (RESEARCH.md Pattern 2):
```go
func (a *Adapter) Start(ctx context.Context) error {
    if a.client.Store.ID == nil {
        // First run — need QR pairing
        qrChan, err := a.client.GetQRChannel(ctx)
        if err != nil {
            return fmt.Errorf("get QR channel: %w", err)
        }
        go func() {
            for evt := range qrChan {
                if evt.Event == "code" {
                    fmt.Println("Scan this QR code or paste into a QR generator:")
                    fmt.Println(evt.Code)
                }
            }
        }()
    }
    return a.client.Connect()
}
```

**startTime field** (CONTEXT.md D-07) — set in `New()` or `Start()`, before `client.Connect()`:
```go
type Adapter struct {
    startTime time.Time
    // ...
}

func New(...) *Adapter {
    return &Adapter{
        startTime: time.Now(), // record before any events can arrive
        // ...
    }
}
```

**Disconnect safety** (RESEARCH.md Pitfall 6, CLAUDE.md): `Disconnect()` is never called from inside the event handler goroutine. It is called by `app.go` after `ctx.Done()` unblocks.

---

### `internal/whatsappadapter/inbound.go` (controller, event-driven)

**Analog:** none in codebase. Pattern sourced entirely from RESEARCH.md.

**Event handler type-switch pattern** (RESEARCH.md Pattern 3):
```go
package whatsappadapter

import (
    "log/slog"
    "os"

    "go.mau.fi/whatsmeow/types/events"
)

func (a *Adapter) onEvent(evt interface{}) {
    switch v := evt.(type) {
    case *events.Connected:
        slog.Info("whatsapp connected")
    case *events.Disconnected:
        slog.Warn("whatsapp disconnected; auto-reconnect in progress")
    case *events.LoggedOut:
        slog.Error("whatsapp logged out", "on_connect", v.OnConnect, "reason", v.Reason)
        // Signal shutdown via stored cancel func — do NOT call Disconnect() here (deadlock)
        a.cancel()
    case *events.StreamReplaced:
        slog.Error("whatsapp stream replaced by another client; shutting down")
        a.cancel() // same pattern as LoggedOut; StreamReplaced is a permanent disconnect
    case *events.PairSuccess:
        slog.Info("whatsapp paired", "jid", v.ID.String())
    case *events.Message:
        a.handleMessage(v)
    }
}
```

**Gate pipeline — order is mandatory** (CLAUDE.md critical invariants, RESEARCH.md Pitfall 3 + anti-patterns):
```go
func (a *Adapter) handleMessage(evt *events.Message) {
    snap := a.cfg.Get() // single atomic load; hold for entire call

    // Gate 0: HistorySync flood filter — FIRST (cheapest; eliminates entire replay)
    if evt.Info.Timestamp.Before(a.startTime) {
        return
    }
    // Gate 1: group JID (SCOPE-01)
    if evt.Info.Chat.String() != snap.Scope.GroupJID {
        return
    }
    // Gate 2: IsFromMe (SCOPE-02) — MUST be after group gate, before text extraction
    if evt.Info.IsFromMe {
        return
    }
    // Gate 3: text messages only (SCOPE-03)
    text := extractText(evt.Message)
    if text == "" {
        return
    }

    slog.Info("message received",
        "group_jid", evt.Info.Chat.String(),
        "sender_jid", evt.Info.Sender.ToNonAD().String(),
        "msg_id", evt.Info.ID,
    )
    // Phase 2+: pass domain.Message to matcher pipeline
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

**JID comparison** — always use `.String()` for comparison; use `.ToNonAD().String()` when comparing sender to owner list (CLAUDE.md Pitfall 7 / critical invariant).

---

### `internal/whatsappadapter/walog.go` (utility, request-response)

**Analog:** none in codebase. Pattern sourced entirely from RESEARCH.md.

**slog → waLog.Logger bridge** (RESEARCH.md Pattern 4):
```go
package whatsappadapter

import (
    "fmt"
    "log/slog"

    waLog "go.mau.fi/whatsmeow/util/log"
)

type slogAdapter struct {
    log *slog.Logger
}

// Verify interface compliance at compile time
var _ waLog.Logger = slogAdapter{}

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

func newWALogger(module string) waLog.Logger {
    return slogAdapter{log: slog.Default().With("module", module)}
}
```

---

## Shared Patterns

### Signal-Aware Context
**Source:** `main.go` lines 12-17
**Apply to:** `cmd/bot/main.go` (verbatim), `internal/app/app.go` (block on `ctx.Done()`)
```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
defer stop()
<-ctx.Done()
```

### Structured slog Logging
**Source:** `main.go` line 14 + CLAUDE.md Logging section
**Apply to:** all files
```go
// Style: plain message string, structured key-value pairs as trailing args
slog.Info("Starting application...")
slog.Info("config reloaded", "path", configPath)
slog.Warn("config reload failed; keeping previous config", "err", err)
slog.Error("whatsapp logged out", "on_connect", v.OnConnect, "reason", v.Reason)
// Never: slog.Info(fmt.Sprintf("config reloaded: %s", path))
```
Match decisions must include `reason` field (CLAUDE.md Logging section) — not applicable in Phase 1 but establish the field-name convention.

### Error Wrapping
**Source:** Go stdlib convention; `loadConfig` stub signals return-error style
**Apply to:** all service functions
```go
// Always wrap with context; never use bare fmt.Errorf("bad") without %w
return nil, fmt.Errorf("open sqlite: %w", err)
return nil, fmt.Errorf("parse yaml: %w", err)
```

### Module Import Paths
**Source:** `go.mod` line 1
**Apply to:** all files
```
github.com/taldoflemis/bot-camomila/internal/config
github.com/taldoflemis/bot-camomila/internal/domain
github.com/taldoflemis/bot-camomila/internal/whatsappadapter
github.com/taldoflemis/bot-camomila/internal/app
```

### Context Propagation
**Source:** `main.go` signal context; RESEARCH.md architecture diagram
**Apply to:** all functions that perform I/O or block
```go
// Pass ctx as first parameter — Go convention
func Run(ctx context.Context, configPath string) error { ... }
func (w *Watcher) Run(ctx context.Context) error { ... }
func openStore(ctx context.Context, dbPath string, ...) { ... }
```

### Hexagonal Import Firewall
**Source:** CLAUDE.md Architecture section
**Apply to:** all packages
- `go.mau.fi/whatsmeow` imports are ONLY allowed in `internal/whatsappadapter/`
- `internal/domain/` must import NO external packages except stdlib
- `internal/config/` must import only: `goccy/go-yaml`, `fsnotify/fsnotify`, `go.mau.fi/whatsmeow/types` (for JID validation), stdlib

---

## No Analog Found

Files with no close match in the codebase (planner uses RESEARCH.md patterns directly):

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/config/store.go` | utility | event-driven | No atomic snapshot pattern exists anywhere in codebase |
| `internal/config/watcher.go` | service | event-driven | No fsnotify usage anywhere in codebase |
| `internal/domain/message.go` | model | transform | No domain layer exists yet |
| `internal/whatsappadapter/client.go` | service | request-response | No whatsmeow usage anywhere in codebase |
| `internal/whatsappadapter/inbound.go` | controller | event-driven | No event handler pattern anywhere in codebase |
| `internal/whatsappadapter/walog.go` | utility | request-response | No logger bridge anywhere in codebase |

All six have complete, concrete patterns in RESEARCH.md (Patterns 1-7 and Code Examples section) that serve as the authoritative implementation template.

---

## Metadata

**Analog search scope:** `/home/flemis/codes/bot-camomila/` (all Go files)
**Files scanned:** 2 Go source files (`main.go`, `config.go`), 1 YAML (`config.example.yaml`), 1 module file (`go.mod`)
**Reason for sparse analogs:** Project is at initial scaffold stage. The two existing Go files are the sole analogs; they cover the signal-context, slog usage, and empty-struct patterns. All other patterns are drawn from RESEARCH.md (which sources from pkg.go.dev-verified library documentation).
**Pattern extraction date:** 2026-05-23
