# Architecture Research

**Domain:** WhatsApp single-group de-escalation reply bot (whatsmeow + Go)
**Researched:** 2026-05-22
**Confidence:** HIGH (whatsmeow patterns, Go stdlib concurrency primitives, fsnotify behavior all verified against official docs / source). MEDIUM on the exact debounce window for fsnotify reloads (empirical tuning).

---

## Standard Architecture

### System Overview (Hexagonal / Ports-and-Adapters, slimmed for a single binary)

```
┌──────────────────────────────────────────────────────────────────────┐
│                          PROCESS (cmd/bot)                            │
│                                                                       │
│   signal.NotifyContext  ─►  app.Run(ctx)  ─►  errgroup.WithContext   │
│                                                                       │
└──────────────────────────────────────────────────────────────────────┘
            │                          │                       │
            ▼                          ▼                       ▼
┌────────────────────┐    ┌────────────────────────┐  ┌──────────────────┐
│  ADAPTERS (in)     │    │   DOMAIN (pure Go)     │  │ ADAPTERS (out)   │
│                    │    │                        │  │                  │
│  whatsappadapter   │───►│  matcher.Pipeline      │─►│  whatsappadapter │
│  (whatsmeow.Client │    │   - normalize          │  │  .Reply(...)     │
│   event handler)   │    │   - fuzzy match        │  │  (SendMessage    │
│                    │    │   - cooldown gate      │  │   w/ ContextInfo)│
│  configwatcher     │───►│   - quiet hours        │  │                  │
│  (fsnotify)        │    │   - kill switch        │  │  log/slog        │
│                    │    │                        │  │                  │
│  ownercommands     │───►│  switch.KillSwitch     │  │                  │
│  (DM filter)       │    │  cooldown.Store        │  │                  │
└────────────────────┘    │  config.Snapshot       │  └──────────────────┘
                          │  (atomic.Pointer)      │
                          └────────────────────────┘
                                      │
                                      ▼
                          ┌────────────────────────┐
                          │     STATE              │
                          │  whatsmeow session     │
                          │   → SQLite (file)      │
                          │  cooldown table        │
                          │   → in-memory map      │
                          │  kill switch           │
                          │   → atomic.Bool        │
                          │  active config         │
                          │   → atomic.Pointer     │
                          └────────────────────────┘
```

The diagram is hexagonal in spirit but a single Go binary in practice: one process, one `main`, one wire-up function. Hexagonal-style separation matters here purely for **testability** — keeping `matcher.Pipeline` ignorant of `whatsmeow.Client` lets the entire matching path run under `go test` without a WA connection.

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|---------------|------------------------|
| `cmd/bot/main.go` | Process entrypoint. Build context, load config, wire adapters, run errgroup, await shutdown. | `signal.NotifyContext` + `errgroup.WithContext` (or plain goroutines + `<-ctx.Done()`). Stays under ~80 lines. |
| `internal/app` | Composition root. `Run(ctx, cfg)` wires every component. Where DI happens. | Constructor functions; no globals. |
| `internal/config` | YAML loading, validation, hot-reload publishing. Owns the `atomic.Pointer[Snapshot]`. | `gopkg.in/yaml.v3` + `fsnotify`. Validates BEFORE swapping. |
| `internal/domain` | Pure-Go value types: `Message`, `MatcherRule`, `Cluster`, `Snapshot`, `MatchResult`. **Zero imports of whatsmeow.** | Plain structs + small constructors. |
| `internal/matcher` | The pipeline: normalize → fuzzy match → cooldown gate → quiet-hours gate → kill-switch gate → pick answer. Returns `MatchResult{Action, Text, ReplyTo}` or `nil`. | Single `Pipeline.Handle(domain.Message) *MatchResult`. Pure function modulo the cooldown store and clock. |
| `internal/cooldown` | Per-matcher + per-(matcher,user) cooldown bookkeeping with TTL eviction. Injectable clock for tests. | `sync.Map` keyed by `(matcherName,userJID)`; background ticker reaps dead entries. |
| `internal/killswitch` | Boolean toggled by owner DM commands. Read in the pipeline. | `atomic.Bool`. |
| `internal/whatsappadapter` | The only package that imports `go.mau.fi/whatsmeow`. Converts `*events.Message` → `domain.Message`; converts `*MatchResult` → `SendMessage` with `ContextInfo` quote. Owns pairing/QR/Connect lifecycle. | `whatsmeow.Client` + `sqlstore.Container`. |
| `internal/ownercommands` | Routes owner DMs (kill, resume, status) to side effects on `killswitch`. | Pattern-matches text against a small command set; ignores non-owner senders. |

The package boundary that matters most: **`internal/domain` and everything it imports must compile with `go test ./internal/matcher/...` without ever touching whatsmeow.** That is the testability bet.

---

## Recommended Project Structure

```
bot-camomila/
├── cmd/
│   └── bot/
│       └── main.go              # entrypoint: signals, ctx, app.Run
├── internal/
│   ├── app/
│   │   └── app.go               # composition root: wires everything
│   ├── config/
│   │   ├── config.go            # struct types (move out of root)
│   │   ├── load.go              # YAML parse + validate
│   │   ├── store.go             # atomic.Pointer[Snapshot] + Get/Swap
│   │   └── watcher.go           # fsnotify directory watcher + debounce
│   ├── domain/
│   │   ├── message.go           # domain.Message (pure)
│   │   ├── rule.go              # MatcherRule, Cluster, Snapshot
│   │   └── result.go            # MatchResult
│   ├── matcher/
│   │   ├── normalize.go         # casefold + accent strip
│   │   ├── fuzzy.go             # Levenshtein wrapper
│   │   ├── pipeline.go          # Pipeline.Handle(msg) → *MatchResult
│   │   └── pipeline_test.go     # ★ pure-Go tests; no network
│   ├── cooldown/
│   │   ├── store.go             # sync.Map + TTL + injectable Clock
│   │   └── store_test.go
│   ├── killswitch/
│   │   └── switch.go            # atomic.Bool wrapper
│   ├── ownercommands/
│   │   └── router.go            # owner DM → command → side effect
│   └── whatsappadapter/
│       ├── client.go            # whatsmeow.Client lifecycle, QR, Connect, Disconnect
│       ├── inbound.go           # events.Message → domain.Message
│       └── outbound.go          # MatchResult → SendMessage(ContextInfo)
├── config.example.yaml
├── go.mod
└── Dockerfile
```

### Structure Rationale

- **`cmd/bot/`** even with a single binary — the convention costs nothing and signals intent. Per the Go layout community, `cmd/` is appropriate when a `main` does non-trivial wire-up, and Docker entrypoints become trivially `./bot`.
- **`internal/`** for everything because nothing here is meant to be imported by other modules. The compiler enforces this.
- **No `pkg/`.** Nothing is library-shaped. Adding `pkg/` is cargo-culting for a single-binary service.
- **`internal/domain` separate from `internal/matcher`** so that the matcher can depend on domain types without dragging in implementation details, and so that the whatsmeow adapter can also map into domain types without depending on matcher.
- **`internal/config` owns the atomic pointer**, not main. The watcher and the snapshot store live together — fewer cross-package references for the hot path.
- **`internal/whatsappadapter` is the import firewall.** Greppable rule: `grep -r "go.mau.fi/whatsmeow" internal/ | grep -v whatsappadapter` should return zero matches. If it ever returns something, the testability invariant has been broken.

---

## Architectural Patterns

### Pattern 1: Atomic Snapshot for Hot-Reloadable Config

**What:** Configuration lives behind an `atomic.Pointer[Snapshot]`. Readers do one atomic load; the watcher builds a new immutable snapshot on disk change and `Store`s it. No locks on the hot path.

**When to use:** Read-mostly config where readers vastly outnumber writers (which is exactly this bot — every incoming message reads the matcher list; YAML changes maybe once an hour).

**Trade-offs:**
- Pro: lock-free reads, zero contention, snapshots are immutable so readers can hold a reference across the entire pipeline without fearing mid-flight mutation.
- Pro: validation happens before swap → invalid YAML never replaces a working config; bot keeps running on the old snapshot.
- Con: requires discipline that the snapshot is **never mutated in place** — only replaced wholesale.

**Example:**

```go
// internal/config/store.go
package config

import "sync/atomic"

type Store struct{ ptr atomic.Pointer[Snapshot] }

func (s *Store) Get() *Snapshot       { return s.ptr.Load() }
func (s *Store) Swap(n *Snapshot)     { s.ptr.Store(n) }

// internal/matcher/pipeline.go (hot path)
func (p *Pipeline) Handle(m domain.Message) *domain.MatchResult {
    snap := p.cfg.Get()              // one atomic load; immutable from here on
    if snap.KillSwitchOn() { return nil }
    for _, rule := range snap.Matchers {
        // ...
    }
}
```

**Why not `sync.Map`?** `sync.Map` is for per-key concurrent access (cooldown is the use case). The whole config is one object — `atomic.Pointer[Snapshot]` is the textbook fit, and is what the Go runtime authors recommend for exactly this scenario.

### Pattern 2: Watch the Directory, Not the File (fsnotify reload)

**What:** Register an fsnotify watch on the *parent directory* of `config.yaml`, filter events by name, debounce, then parse + validate + swap.

**When to use:** Always, for any fsnotify-based config reload. Direct file watches break the moment any editor (vim, VS Code, sed via temp file, Docker mount swap, `kubectl apply` on a ConfigMap) does an atomic rename, which is most of the time.

**Trade-offs:**
- Pro: survives atomic-write editors and Docker volume re-mounts.
- Pro: a single watcher catches Create + Write + Rename + Chmod uniformly.
- Con: must filter events by `Event.Name` because the directory may contain other files (so put the YAML in its own dir if possible — `/etc/bot-camomila/config.yaml` style).

**Example:**

```go
// internal/config/watcher.go
w, _ := fsnotify.NewWatcher()
w.Add(filepath.Dir(path))               // ★ directory, not file

var timer *time.Timer
for {
    select {
    case ev := <-w.Events:
        if filepath.Base(ev.Name) != filepath.Base(path) { continue }
        if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 { continue }
        if timer != nil { timer.Stop() }
        timer = time.AfterFunc(200*time.Millisecond, func() {
            snap, err := Load(path)
            if err != nil { slog.Error("reload failed; keeping old", "err", err); return }
            store.Swap(snap)
            slog.Info("config reloaded")
        })
    case <-ctx.Done():
        return w.Close()
    }
}
```

200 ms debounce absorbs the multi-event bursts editors emit on save. **Tune this window in integration tests against your actual editor/deploy flow.**

### Pattern 3: Sharded TTL Cooldown Store with Background Reaper

**What:** Cooldowns live in a `sync.Map` keyed by a composite (matcher name + user JID, plus a separate per-matcher entry with a sentinel user). Each value is an expiry timestamp. A single goroutine ticks every N seconds and deletes expired entries.

**When to use:** Read-mostly, key-disjoint, in-memory TTL state where loss on restart is acceptable (which the PROJECT.md explicitly says it is).

**Trade-offs:**
- Pro: lock-free reads of the common case (entry absent → not in cooldown). `sync.Map` shines exactly here.
- Pro: bounded memory because the reaper runs in the background; no entry lives past 2× its TTL.
- Con: `sync.Map` is slower than `map + sync.RWMutex` for write-heavy small caches. Cooldown is read-heavy with a small write rate, so `sync.Map` wins. Re-evaluate only if profiling shows otherwise.
- Con: clock skew on `time.Now()` calls — inject a `Clock` interface so tests can drive time deterministically.

**Example:**

```go
// internal/cooldown/store.go
type Store struct {
    m     sync.Map // map[key]time.Time
    clock Clock
}

type key struct{ Matcher, User string } // User = "" for per-matcher cooldown

func (s *Store) Allow(k key, ttl time.Duration) bool {
    now := s.clock.Now()
    if v, ok := s.m.Load(k); ok && v.(time.Time).After(now) { return false }
    s.m.Store(k, now.Add(ttl))
    return true
}

func (s *Store) reap(ctx context.Context, every time.Duration) {
    t := time.NewTicker(every); defer t.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case now := <-t.C:
            s.m.Range(func(k, v any) bool {
                if v.(time.Time).Before(now) { s.m.Delete(k) }
                return true
            })
        }
    }
}
```

Reap interval ≈ 1/5 of the shortest cooldown TTL. For a bot with 30s–5min cooldowns, every 10s is fine.

**Race-free Allow:** the `Load` + `Store` above is not atomically check-then-set. For this domain (cooldowns are best-effort), that's acceptable — a tiny race window can let two replies through, and there is only one process. If strictness is ever required, switch to `LoadOrStore` with a wrapper that interprets the loaded value.

### Pattern 4: Kill Switch as `atomic.Bool`

**What:** A single `atomic.Bool`, set by owner DM commands, read by the pipeline gate.

**When to use:** Single-bit global state that the hot path reads on every event.

**Trade-offs:**
- Pro: cheapest possible read; no allocation, no syscall.
- Pro: trivially correct, no goroutine lifecycle to manage.
- Con: you cannot block on it (you can't `select` on `atomic.Bool`). That's fine here — we are gating, not signaling shutdown.

Channels would be wrong here: kill switch is **state**, not an event. Channels signal that something *happened*; `atomic.Bool` answers *what is true right now*.

```go
// internal/killswitch/switch.go
type Switch struct{ off atomic.Bool }
func (s *Switch) On()  { s.off.Store(false) }
func (s *Switch) Off() { s.off.Store(true) }
func (s *Switch) IsOff() bool { return s.off.Load() }
```

### Pattern 5: Adapter Translation at the Boundary

**What:** The whatsmeow adapter is the **only** place that knows about `*events.Message`, `*waProto.Message`, `types.JID`, etc. Inbound: it builds a `domain.Message{ID, GroupJID, SenderJID, Text, QuotedText, Timestamp, IsFromOwner}`. Outbound: it consumes a `domain.MatchResult{ReplyText, ReplyToID, ReplyToParticipant, Chat}` and constructs the `ContextInfo`-bearing `SendMessage` call.

**When to use:** Whenever a third-party SDK has a sprawling type surface that you don't want leaking into business logic. whatsmeow's proto types are exactly this case.

**Trade-offs:**
- Pro: matcher/cooldown/owner-commands packages compile and test without whatsmeow installed. `go test ./internal/...` from a fresh checkout works.
- Pro: replacing whatsmeow (if it ever breaks) means rewriting one adapter, not the whole app.
- Con: small mapping cost per event (a struct copy). Irrelevant at WhatsApp message rates.

```go
// internal/whatsappadapter/inbound.go
func toDomain(evt *events.Message, ownerJIDs map[string]bool) domain.Message {
    text := evt.Message.GetConversation()
    if ext := evt.Message.GetExtendedTextMessage(); ext != nil { text = ext.GetText() }
    var quoted string
    if ci := evt.Message.GetExtendedTextMessage().GetContextInfo(); ci != nil {
        quoted = ci.GetQuotedMessage().GetConversation()
    }
    return domain.Message{
        ID: evt.Info.ID, GroupJID: evt.Info.Chat.String(),
        SenderJID: evt.Info.Sender.String(),
        Text: text, QuotedText: quoted,
        Timestamp: evt.Info.Timestamp,
        IsFromOwner: ownerJIDs[evt.Info.Sender.ToNonAD().String()],
    }
}
```

---

## Data Flow

### Incoming Message Path (hot path)

```
WhatsApp server
    │
    ▼
whatsmeow.Client.AddEventHandler
    │  (single goroutine per event, dispatched by whatsmeow)
    ▼
whatsappadapter.onEvent(evt)
    │   type-switch on *events.Message; else discard
    ▼
GROUP FILTER: evt.Info.Chat.String() == cfg.Get().TargetGroupJID ?
    │   no → drop (also: owner DMs branch off to ownercommands here)
    ▼
toDomain(evt) → domain.Message
    │
    ▼
matcher.Pipeline.Handle(msg)
    │
    │   snap := cfg.Get()                    ← one atomic load
    │   if snap.KillSwitch.IsOff()  → nil
    │   if snap.InQuietHours(now)   → nil
    │
    │   normalize(msg.Text, msg.QuotedText)
    │
    │   for each rule in snap.Matchers:
    │       if fuzzyMatch(normalized, rule.Words, rule.Distance):
    │           if !cooldown.Allow(rule, msg.Sender) → continue
    │           pick := rand(rule.Cluster.Answers)
    │           return &MatchResult{ ReplyText: render(pick, msg),
    │                                 ReplyToID: msg.ID,
    │                                 ReplyToParticipant: msg.SenderJID }
    │   return nil
    │
    ▼
whatsappadapter.reply(result)
    │   build waProto.Message with ExtendedTextMessage{Text, ContextInfo{
    │       StanzaId, Participant, QuotedMessage}}
    ▼
whatsmeow.Client.SendMessage(ctx, chat, msg)
    │
    ▼
WhatsApp server (threaded reply visible in the group)
```

### Hot-Reload Path

```
config.yaml changes on disk
    │
    ▼
fsnotify directory watcher fires Write|Create|Rename event
    │
    ▼
Debounce timer (200ms) — coalesces editor's multi-event burst
    │
    ▼
config.Load(path) → parse YAML, validate (required fields, JID format,
                    cooldown durations sane, all matcher clusters resolve)
    │
    ├── error → slog.Error("reload failed"); keep old snapshot; bot continues
    │
    └── ok    → store.Swap(newSnapshot)
                    │
                    ▼
                Subsequent matcher.Handle calls atomic.Load the new snapshot.
                Any in-flight Handle call keeps its old snapshot reference
                (immutable, safe).
```

### Owner Command Path

```
DM arrives → whatsappadapter sees evt.Info.Chat is a 1:1 (not a group)
    │
    ▼
ownercommands.Route(senderJID, text)
    │
    ├── senderJID ∉ ownerJIDs → drop silently
    │
    ├── text == "!pause"      → killswitch.Off(); reply "🌼 paused"
    ├── text == "!resume"     → killswitch.On();  reply "🌼 resumed"
    └── text == "!status"     → reply summary (snapshot version, paused?, etc.)
```

### Startup Sequence

```
main()
  ctx, stop := signal.NotifyContext(...)
  cfg, _ := config.Load("config.yaml")
  cfgStore := config.NewStore(cfg)
  configWatcher := config.NewWatcher(cfgStore, "config.yaml")     // not started
  cooldownStore := cooldown.New(realClock)
  killSwitch := killswitch.New()
  pipeline := matcher.NewPipeline(cfgStore, cooldownStore, killSwitch)

  waContainer, _ := sqlstore.New(ctx, "sqlite3", dbPath, dbLog)
  device, _ := waContainer.GetFirstDevice(ctx)
  waClient := whatsmeow.NewClient(device, clientLog)
  adapter := whatsappadapter.New(waClient, pipeline, cfgStore, ownerCommands)

  if waClient.Store.ID == nil { runQRPairingFlow(ctx, waClient) }  // first run only

  waClient.AddEventHandler(adapter.OnEvent)

  errgroup.Go: configWatcher.Run(ctx)
  errgroup.Go: cooldownStore.Reap(ctx, 10*time.Second)
  errgroup.Go: waClient.Connect()  // wraps Connect + blocks until ctx.Done

  <-ctx.Done()
  shutdownCtx, _ := context.WithTimeout(context.Background(), 10*time.Second)
  waClient.Disconnect()
  errgroup.Wait()
```

**Crucial:** use `context.Background()` (not the already-cancelled `ctx`) as the parent of the shutdown timeout — a common bug that makes timeouts expire immediately.

---

## Scaling Considerations

This bot is intentionally single-tenant. "Scale" mostly means resilience, not throughput.

| Scale | Architecture Adjustments |
|-------|--------------------------|
| Single group, ≤10 matchers (v1 target) | Current design is exactly right. No changes needed. |
| Many groups (out of scope per PROJECT.md) | Would force: per-group config, multi-tenant cooldown keys including group JID, per-group kill switches. Today's `domain.Message` already carries `GroupJID`, so the extension point exists without refactor. |
| Persistent cooldown across restarts (not v1) | Add a `cooldown.Store` implementation backed by SQLite (separate table from whatsmeow's session). The interface stays. |
| LLM-generated replies (out of scope) | Add a new `matcher.Responder` interface; current random-pick becomes one impl. Pipeline stays the same. |

### Scaling Priorities (i.e. "what breaks first")

1. **First bottleneck: the WhatsApp account itself.** A misconfigured matcher that spams will get the number flagged before any Go code becomes the limit. Mitigation: cooldowns are first-class, quiet hours, kill switch — all already in v1.
2. **Second bottleneck: cooldown map growth** if user JIDs are unbounded. With one group of, say, 100 members and 10 matchers, that's 1000 entries max — laughably small. The reaper keeps it bounded anyway.
3. **Third bottleneck: fsnotify event storm** under pathological deploy scripts that touch the file many times per second. The debounce window handles this.

There is no path here that requires sharding, queues, or microservices for v1 or v2.

---

## Concurrency Invariants (for the roadmapper to preserve)

These are the rules that, if broken, will cause subtle bugs:

1. **The config snapshot is immutable once published.** Reload builds a brand-new `*Snapshot` and `atomic.Store`s it. Nothing ever mutates an existing snapshot. If the roadmap ever proposes "in-place tweak the matcher list," reject it — replace the whole snapshot instead.

2. **All reads on the hot path use a single snapshot reference.** The pipeline loads `cfg.Get()` exactly once at the top of `Handle` and uses that pointer for the rest of the call. Don't re-load mid-pipeline — that would invite torn reads where matcher selection used the old config but cluster lookup used the new one.

3. **The cooldown `Allow` call is the side-effecting check; it is the gate.** Pipeline must call `Allow` exactly once per (rule, user) per event, after match succeeds, before reply construction. Not before match (waste) and not after reply (race window).

4. **whatsmeow event handlers must not block.** whatsmeow dispatches events on goroutines but a slow handler will starve later events. Reply sending is itself a network call; if it ever becomes slow, push the `MatchResult → SendMessage` step into a small buffered channel + worker goroutine. Not needed for v1.

5. **The kill switch is checked inside the pipeline, not at the adapter.** Otherwise owner commands (which also flow through the adapter) would be silenced when paused — and we want owner commands to *resume* the bot.

6. **Owner JIDs must be normalized before comparison.** WhatsApp JIDs have device suffixes (`:N`); always call `.ToNonAD()` before set membership check. Bug source #1 for owner-only commands.

7. **fsnotify watcher must watch the directory, not the file.** Direct file watches die on the first atomic-rename save.

8. **Shutdown order: stop accepting input first, then drain, then close stores.** `waClient.Disconnect()` first (stops new events), then `errgroup.Wait()` (drains in-flight Handle calls), then the SQLite container closes via its own deferred Close. The `signal.NotifyContext` already wired in `main.go` is the right primitive.

9. **The cooldown reaper must respect `ctx.Done()`.** Otherwise it leaks a goroutine on every test that spins up a `Store`.

10. **Validation precedes swap.** Never `atomic.Store` an unvalidated snapshot. A typo in YAML should log an error, keep the bot running on the previous config, and *not* take down the matcher loop on a nil dereference.

---

## Anti-Patterns

### Anti-Pattern 1: Importing whatsmeow from the matcher package

**What people do:** Pass `*events.Message` directly into `matcher.Handle` because "it has all the fields we need."
**Why it's wrong:** Couples the matcher to a fast-moving third-party SDK. Makes unit testing the matcher require constructing whatsmeow protos by hand (which is painful, since they're generated proto types with internal flags). Breaks the testability invariant in PROJECT.md.
**Do this instead:** The adapter maps to `domain.Message` first. Matcher accepts the domain type only. `grep -r "go.mau.fi/whatsmeow" internal/matcher/` must return nothing — make this a CI check eventually.

### Anti-Pattern 2: Mutex-protected config struct on the hot path

**What people do:** `cfg.mu.RLock(); rules := cfg.Matchers; cfg.mu.RUnlock()` on every event.
**Why it's wrong:** RWMutex contention is real under load; you cannot hold the slice past Unlock without copying; reload must take the write lock and block readers. None of this is needed when the config is immutable.
**Do this instead:** `atomic.Pointer[Snapshot]`. Reads are one atomic load; reload is one atomic store; no lock at all.

### Anti-Pattern 3: Watching the YAML file directly with fsnotify

**What people do:** `watcher.Add("config.yaml")` and assume it will keep firing.
**Why it's wrong:** Any atomic-rename save (vim, VS Code with the right setting, `mv tmp config.yaml`, Docker volume update) destroys the inode being watched; subsequent saves emit no events. Bot silently stops reloading and nobody notices until a matcher tweak doesn't apply.
**Do this instead:** Watch the parent directory; filter events by `filepath.Base(ev.Name) == "config.yaml"`. Debounce.

### Anti-Pattern 4: Channel-based kill switch

**What people do:** A `chan struct{}` that the pipeline `select`s on every event.
**Why it's wrong:** `select` on every event is wasteful, and a channel models "an event happened" not "a state holds." You'd still need to reconstruct the boolean state separately. Two sources of truth.
**Do this instead:** `atomic.Bool`. One read per event, zero allocations, trivially correct.

### Anti-Pattern 5: Cooldown TTL evaluated only on read with no reaper

**What people do:** Store `(deadline)` per key, check on read, never delete.
**Why it's wrong:** Keys accumulate forever. With per-user keys this can grow unbounded as group membership churns. The bot's memory footprint slowly drifts up over weeks of uptime — exactly the kind of bug that won't show in dev.
**Do this instead:** Background reaper goroutine, ticker-driven, bounded by `ctx.Done()`. The fact that memory is bounded by a `runtime` assertion is what makes "in-memory cooldown" defensible for v1.

### Anti-Pattern 6: Doing the SendMessage inside the type-switch handler synchronously, blocking the whatsmeow dispatcher

**What people do:** The event handler calls `client.SendMessage(...)` inline.
**Why it's wrong:** SendMessage is a network round-trip. While it's in flight, this event handler invocation is blocked — and any other in-flight events on the same goroutine are queued. For a low-traffic group this is fine; for a chatty group with a transient WhatsApp slowdown, events back up.
**Do this instead:** v1 inline is acceptable. If it ever becomes a problem, drop a small `chan *domain.MatchResult` between pipeline and adapter outbound, with a single worker goroutine doing the send. Document this in the roadmap as a v2 candidate.

### Anti-Pattern 7: Group filter inside the matcher package

**What people do:** `matcher.Pipeline` checks `if msg.GroupJID != snap.TargetGroup`.
**Why it's wrong:** Mixes a transport-level concern (which group are we listening to?) with business logic (does this text match a matcher?). Makes the matcher harder to test (you need a real GroupJID in every test fixture) and to extend later (multi-group would mean ripping it out).
**Do this instead:** Filter at the adapter. The matcher receives only messages from the target group; it doesn't care which one. Owner DMs are routed separately at the same adapter layer.

---

## Suggested Build Order (component dependencies)

This is the order to **prove things work**. Each step produces a runnable artifact that the next step builds on.

| # | Build | Why First | Verified By |
|---|-------|-----------|-------------|
| 1 | `internal/config` (load + validate + struct types) | Everything else needs to consume config; this is pure-Go and fully unit-testable. | `go test ./internal/config -run TestLoadValidates` |
| 2 | `internal/domain` types | Tiny, dependency-free, but required signatures for steps 3–5. | Compiles. |
| 3 | `internal/whatsappadapter` skeleton: pairing, Connect, QR, Disconnect, event handler that just logs | Until we can pair, nothing about matching is verifiable. **Pairing being a one-time manual step makes it the riskiest setup task — do it early.** | Run the bot, scan QR, see "connected" in logs, restart, see "reusing session." |
| 4 | Group filter + adapter inbound mapping (`*events.Message` → `domain.Message`) | Now we can see real domain messages in logs from the target group only. Confirms JID filtering works before any matching logic. | Send test messages in/out of the target group; only target-group messages should log. |
| 5 | `internal/matcher.Pipeline` (normalize + fuzzy match + answer pick), wired with no cooldown, no quiet hours, no kill switch | The actual product behavior. Test in pure Go first (`pipeline_test.go`), then plug into the adapter. | `go test ./internal/matcher` passes; bot replies to test messages. |
| 6 | `internal/whatsappadapter` outbound (`MatchResult` → threaded `SendMessage`) | Closes the loop; bot now replies threaded. | Reply appears in target group as a quote-reply of the trigger. |
| 7 | `internal/cooldown` store + integrate into pipeline | Bot now stops spamming. This is a soft requirement that becomes hard the second you test in a real group. | Spam-test the matcher; second reply within TTL should not appear. |
| 8 | Quiet hours gate in pipeline | Trivial once cooldown is in. | Time-shift unit test. |
| 9 | `internal/killswitch` + `internal/ownercommands` (owner DM routing) | Now the bot is operable — you can pause it remotely without restart. | DM `!pause` from owner; trigger word; no reply. DM `!resume`; trigger word; reply. |
| 10 | `internal/config.Watcher` (fsnotify) + integration into store | Final operational nicety. Risky because of editor/atomic-write quirks; leave for last so a broken watcher doesn't block earlier work. | Edit YAML; logs show "config reloaded"; new matcher word triggers without restart. |
| 11 | Dockerfile + volume mounts (SQLite session + YAML) | Deploy. | `docker run` survives a container restart without re-pairing. |

**Hard prerequisites between steps:**

- 3 blocks 4 (no events without a connection).
- 4 blocks 6 (no outbound test without inbound flowing).
- 5 should land before 6 only in the trivial form ("reply to any match"); the full pipeline with cooldown can land after 6 — *but* deploying step 6 without step 7 in any real group is a footgun. Treat 5+6+7 as a single shippable bundle.
- 9 depends on 4 (owner DM identification) and on `killswitch` existing as a referenced gate in 5's pipeline (even if always-on initially). Bake the kill-switch read into step 5's pipeline from day one with a no-op implementation; swap in the real switch in step 9.
- 10 is independent and can be deferred indefinitely; the bot is fully functional with restart-on-config-change. List as a separate small phase.

---

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| WhatsApp (multi-device) | `go.mau.fi/whatsmeow` websocket client. QR pairing on first run; session persisted in SQLite. | One device per phone number. Pairing must happen interactively (terminal QR render) the first time; subsequent starts reuse session. Account is per-number, not per-app. |
| SQLite (whatsmeow session only) | `go.mau.fi/whatsmeow/store/sqlstore` Container. Driver: `modernc.org/sqlite` (pure Go, no CGO — Dockerfile stays simple) or `github.com/mattn/go-sqlite3` (CGO, faster but complicates static binaries). | Recommend `modernc.org/sqlite` for v1 to keep Docker image minimal. App owns no other tables. |
| Filesystem (YAML config) | `os.ReadFile` + `gopkg.in/yaml.v3` + `fsnotify` directory watch. | Mount as a Docker volume. Don't bake the YAML into the image. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|--------------|-------|
| `whatsappadapter` → `matcher.Pipeline` | Direct function call passing `domain.Message`. Synchronous. | Pipeline returns `*MatchResult` or `nil`. Adapter sends if non-nil. |
| `matcher.Pipeline` → `config.Store` | `cfg.Get()` returns `*Snapshot` (atomic load). | One load per `Handle` call. |
| `matcher.Pipeline` → `cooldown.Store` | `cooldown.Allow(key, ttl) bool`. Mutates store. | Side-effecting; call exactly once per matched rule. |
| `matcher.Pipeline` → `killswitch.Switch` | `killswitch.IsOff() bool`. | First gate, before any matching work. |
| `ownercommands.Router` → `killswitch.Switch` | `switch.On()` / `switch.Off()`. | Only place that writes to the switch. |
| `config.Watcher` → `config.Store` | `store.Swap(newSnapshot)`. | Only place that writes to the snapshot store after init. |
| `whatsappadapter` → `ownercommands.Router` | Routed at the adapter when `evt.Info.Chat` is a 1:1 with a known owner JID. | DMs from non-owners are dropped silently at this boundary. |
| `app` → everything | Constructor wiring at startup. No service locator, no globals. | Each component is testable in isolation by passing fakes for its dependencies. |

---

## Sources

- [whatsmeow (go.mau.fi/whatsmeow) — pkg.go.dev](https://pkg.go.dev/go.mau.fi/whatsmeow)
- [whatsmeow events package — pkg.go.dev](https://pkg.go.dev/go.mau.fi/whatsmeow/types/events)
- [whatsmeow send.go (source)](https://github.com/tulir/whatsmeow/blob/main/send.go)
- [whatsmeow sqlstore container.go (source)](https://github.com/tulir/whatsmeow/blob/main/store/sqlstore/container.go)
- [whatsmeow qrchan.go (source)](https://github.com/tulir/whatsmeow/blob/main/qrchan.go)
- [whatsmeow discussion #148 — quoted replies / ContextInfo](https://github.com/tulir/whatsmeow/discussions/148)
- [fsnotify — pkg.go.dev](https://pkg.go.dev/github.com/fsnotify/fsnotify)
- [fsnotify issue #214 — watching files survives rename only via dir watch](https://github.com/fsnotify/fsnotify/issues/214)
- [viper issue #142 — WatchConfig and editor save quirks](https://github.com/spf13/viper/issues/142)
- [sync/atomic — pkg.go.dev](https://pkg.go.dev/sync/atomic)
- [Understanding Atomic Operations in Go (Leapcell)](https://leapcell.io/blog/understanding-atomic-operations-in-go-with-sync-atomic)
- [Building Robust Go Applications with Hexagonal Architecture (Leapcell)](https://leapcell.io/blog/building-robust-go-applications-with-hexagonal-architecture)
- [Standard Go Project Layout (golang-standards)](https://github.com/golang-standards/project-layout)
- [Graceful Shutdown in Go: Practical Patterns (VictoriaMetrics)](https://victoriametrics.com/blog/go-graceful-shutdown/)
- [TTL Cache with Automatic Cleanup (Go Patterns)](https://go-patterns.dev/stability/caching/with-expiration)
- [How fsnotify gives Go programs cross-platform file watching](https://www.bytesizego.com/blog/how-fsnotify-gives-go-programs-cross-platform-file-watching)

---
*Architecture research for: WhatsApp single-group de-escalation bot (whatsmeow + Go 1.26.3)*
*Researched: 2026-05-22*
