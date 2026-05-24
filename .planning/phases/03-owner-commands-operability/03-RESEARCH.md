# Phase 3: Owner Commands & Operability - Research

**Researched:** 2026-05-24
**Domain:** Go — whatsmeow group info API, hexagonal command dispatch, atomic kill switch wiring
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Pass `*killswitch.Switch` directly to `whatsappadapter.New()` alongside the pipeline: `whatsappadapter.New(cfgStore, pipe, ks)`. Adapter holds both `*pipeline.Pipeline` and `*killswitch.Switch`. Owner command handler calls `ks.Pause()` / `ks.Resume()` directly. No indirection through the pipeline.
- **D-02:** `app.go` already creates `ks := killswitch.New()` and passes it to `pipeline.New(ks, ...)`. The only change is also passing `ks` to `whatsappadapter.New()`.
- **D-03:** `owner_jids` stays in `ListenerConfig` (per-listener, no schema promotion). No global owner list. Command auth: check sender JID (`.ToNonAD()`) against `listener.OwnerJIDs`. Authorized if match found.
- **D-04:** New optional field `allow_admin_commands: bool` in `ListenerConfig` (default `false`). When `true`, group admins (`IsAdmin == true OR IsSuperAdmin == true` from whatsmeow `GetGroupInfo`) may also issue commands, in addition to owner JIDs.
- **D-05:** If `allow_admin_commands: true`, adapter calls `client.GetGroupInfo(groupJID)` to resolve sender role. This is a network call — acceptable for a rarely-triggered command path.
- **D-06:** Auth check order: (1) sender JID in `listener.OwnerJIDs` → authorized immediately, no network call. (2) if `allow_admin_commands: true` → call GetGroupInfo, check IsAdmin/IsSuperAdmin. Non-authorized senders are silently ignored (no ack, no log at WARN — debug only).
- **D-07:** Commands trigger from the configured group, NOT DMs. Owner JID lookup has group context (the listener), so no DM routing or DM detection is needed.
- **D-08:** Command check happens **before the pipeline** in `handleMessage`, after gates 0–3 (history sync, scope, self-message, text-only). If text matches `!pause` or `!resume` (case-insensitive, trimmed), run auth check and dispatch — skip the matcher pipeline entirely. Commands never enter the pipeline.
- **D-09:** Command parsing: `strings.TrimSpace(strings.ToLower(text))` == `"!pause"` or `"!resume"`. Exact match after trim+lowercase only. No partial or prefix matching.
- **D-10:** Ack is a threaded reply to the command message in the group (same pattern as matcher replies: `ExtendedTextMessage` with `ContextInfo`). Reply text: `"paused"` for `!pause`, `"resumed"` for `!resume`. Brief, unambiguous.
- **D-11:** Ack is sent with a goroutine — no jitter needed for command acks, but goroutine is mandatory to avoid blocking the event handler.
- **D-12:** `internal/ownercommands/` is a pure Go package (no whatsmeow imports). Its interface: receives parsed command string + `*killswitch.Switch` + returns ack string. The adapter handles all whatsmeow interaction (admin lookup, sending ack). The package can be thin — primarily exists to keep command dispatch logic testable outside the adapter.
- **D-13:** The adapter pre-resolves auth (owner JID check + optional admin check) and only calls `ownercommands.Handle(cmd, ks)` when authorized. The ownercommands package never sees unauthorized calls.

### Claude's Discretion

- Logging: every command attempt (authorized and silently dropped) should be logged with `reason` field per CLAUDE.md logging invariants: `owner_command` for authorized execution, `owner_command_denied` for unauthorized (debug level only).
- The `allow_admin_commands` field defaults to `false` and must not affect existing configs that omit it (YAML zero-value safe).

### Deferred Ideas (OUT OF SCOPE)

None — discussion stayed within phase scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OWNER-01 | Bot recognizes a hardcoded list of owner JIDs in YAML config; only those JIDs may issue commands. | `ResolvedListener.OwnerJIDs` already exists in config snapshot; `.ToNonAD().String()` comparison pattern verified in codebase. |
| OWNER-02 | (Overridden by D-07) Commands accepted from the configured group, not DMs. | No DM routing needed; `listener` from Gate 1 provides the group context already. |
| OWNER-03 | Bot implements `!pause` and `!resume`; `!pause` flips atomic kill switch, `!resume` clears it. | `killswitch.Switch.Pause()` / `Resume()` / `IsPaused()` API verified in source. `ownercommands.Handle()` calls them. |
| OWNER-04 | When kill switch is set, pipeline drops every incoming group message; owner commands still process (so `!resume` works). | Command short-circuit (D-08) sits before `a.pipeline.Handle()` call; verified in `inbound.go` line 122. Kill switch gate is inside pipeline (pipeline.go line 58), not in the adapter gate chain. |
| OWNER-05 | Bot acks every owner command with a brief reply ("paused" / "resumed"). | `sendReply()` pattern in `inbound.go` lines 147–182 reusable as `sendCommandAck()`; same `ExtendedTextMessage` + `ContextInfo` shape. |
| OWNER-06 | Kill switch state lives outside the config snapshot; hot-reload does not reset it. | `killswitch.Switch` is an `atomic.Bool` allocated once in `app.go`; config snapshot never holds a reference to it. Verified in `app.go` and `killswitch.go`. |
</phase_requirements>

---

## Summary

Phase 3 adds owner-only runtime control: sending `!pause` or `!resume` in the configured WhatsApp group pauses or resumes the bot's matcher pipeline without a process restart. The kill switch already exists (`internal/killswitch/`) and is already wired into the pipeline; this phase threads it into the adapter so the adapter can call `Pause()`/`Resume()` directly.

The implementation is a pure surgical extension of existing code. Four change sites cover the entire phase: (1) `ListenerConfig`/`ResolvedListener` get `AllowAdminCommands bool`; (2) `whatsappadapter.New()` receives `*killswitch.Switch` as a third argument; (3) `handleMessage()` gets a command short-circuit block after gate 3; (4) a new `internal/ownercommands/` package provides a thin, testable `Handle()` function that owns the state-machine logic. No new dependencies are required. No new data structures beyond what already exists.

The critical ordering invariant: the command short-circuit must execute **after** gates 0–3 (timestamp filter, listener lookup, self-message drop, text-only filter) but **before** `a.pipeline.Handle()`. This guarantees `!resume` reaches the handler even when the kill switch is active, because the kill switch gate lives inside the pipeline, not in the adapter's pre-pipeline gates.

**Primary recommendation:** Implement in three sequenced plans — config schema extension, ownercommands package + adapter wiring, end-to-end verification.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Command text parsing (`!pause`/`!resume` detection) | `whatsappadapter` | — | Runs in `handleMessage()` after text extraction (Gate 3 already provides the `text` string). |
| Auth check — owner JID | `whatsappadapter` | — | Requires `listener.OwnerJIDs` from config snapshot; adapter holds `*config.Store`. |
| Auth check — group admin lookup | `whatsappadapter` | — | Calls `client.GetGroupInfo()` (whatsmeow); must stay inside the whatsmeow boundary. |
| Kill switch state change | `ownercommands` | — | Pure Go; calls `ks.Pause()` / `ks.Resume()`; no whatsmeow dependency. |
| Ack reply send | `whatsappadapter` | — | Uses `client.SendMessage()`; must stay inside the whatsmeow boundary. |
| Kill switch allocation + wiring | `app` (composition root) | — | One `killswitch.New()` in `app.Run()`, passed to both `pipeline.New()` and `whatsappadapter.New()`. |
| Kill switch gate (drop messages when paused) | `pipeline` | — | Already implemented in `pipeline.Handle()` Gate 1. No change needed. |

---

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.mau.fi/whatsmeow` | `v0.0.0-20260516102357-8d3700152a69` | WhatsApp multi-device protocol client; provides `GetGroupInfo()` and `SendMessage()` | Already in project; only package allowed to import whatsmeow per hexagonal constraint. [VERIFIED: go.mod] |
| `sync/atomic` (stdlib) | Go 1.26.3 | `atomic.Bool` backing the kill switch | Already in use; zero new deps. [VERIFIED: killswitch.go] |
| `strings` (stdlib) | Go 1.26.3 | `TrimSpace`/`ToLower` for command parsing (D-09) | Standard library; no dep needed. [VERIFIED: existing inbound.go usage] |
| `log/slog` (stdlib) | Go 1.26.3 | Structured logging for command decisions | Project-wide logging standard. [VERIFIED: CLAUDE.md] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `context` (stdlib) | Go 1.26.3 | `context.WithTimeout` for `GetGroupInfo` network call | Required for the admin-check code path when `allow_admin_commands: true`. [ASSUMED] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Calling `GetGroupInfo` per command | Caching group participants in memory | Cache adds complexity, stale data risk; command path is rare — live fetch is correct choice per D-05. |
| Thin `ownercommands` package | Inline logic in `handleMessage` | Inline breaks hexagonal boundary test coverage; package is required by D-12. |

**Installation:** No new packages needed. All functionality is available from existing dependencies.

---

## Package Legitimacy Audit

No new external packages are introduced in this phase. All packages are from the Go standard library or are already declared in `go.mod`.

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

---

## Architecture Patterns

### System Architecture Diagram

```
WhatsApp event
      │
      ▼
onEvent() [whatsappadapter]
      │
      ▼
handleMessage()
      │
      ├── Gate 0: timestamp filter (drop HistorySync)
      ├── Gate 1: listener lookup (drop unknown group)
      ├── Gate 2: self-message drop
      ├── Gate 3: text-only filter
      │
      ├── [NEW] Command short-circuit ─────────────────────────────┐
      │         │                                                    │
      │    is "!pause"/"!resume"?                                   │
      │         │ YES                                               │
      │    auth check (owner JID or admin lookup)                  │
      │         │                                                    │
      │    ownercommands.Handle(cmd, ks) ─── ks.Pause()/Resume()  │
      │         │                                                    │
      │    go sendCommandAck(evt, ackText) ──────────────────────► ack reply sent to group
      │         │ (return — skip pipeline)                          │
      │         │ NO ─────────────────────────────────────────────┘
      │
      ▼
pipeline.Handle(msg, snap, matchers)
      │
      ├── Kill switch gate (IsPaused → drop)
      ├── Quiet hours gate
      ├── Match gate
      ├── Cooldown gate
      └── Rate cap gate
            │
            ▼
      go sendReply(evt, answer)
```

### Recommended Project Structure

```
internal/
  ownercommands/
    ownercommands.go      # Handle(cmd string, ks *killswitch.Switch) string
    ownercommands_test.go # table-driven tests for pause/resume/unknown
  whatsappadapter/
    client.go             # Adapter struct (add ks field), New() signature change
    inbound.go            # handleMessage() command short-circuit, sendCommandAck()
  config/
    config.go             # ListenerConfig + ResolvedListener: add AllowAdminCommands bool
    load.go               # validate(): propagate AllowAdminCommands to ResolvedListener
  app/
    app.go                # whatsappadapter.New(cfgStore, pipe, ks) — add ks argument
config.example.yaml       # add allow_admin_commands: false to listener example
config.schema.json        # add allow_admin_commands boolean property to listener schema
```

### Pattern 1: Command Short-Circuit in handleMessage

**What:** After the existing Gate 3 text-only filter, check if the normalized text exactly matches `"!pause"` or `"!resume"`. If so, run auth and dispatch, then return — never call `a.pipeline.Handle()`.

**When to use:** Any in-band control command that must bypass the matcher pipeline.

**Example:**
```go
// Source: inbound.go (Phase 3 addition, after Gate 3 text check)

// Command short-circuit — runs BEFORE pipeline (D-08).
// Must execute here so !resume reaches the handler even when kill switch is active.
normalized := strings.TrimSpace(strings.ToLower(text))
if normalized == "!pause" || normalized == "!resume" {
    a.handleOwnerCommand(evt, snap, listener, normalized)
    return
}
```

### Pattern 2: Owner Auth — Two-Tier Check

**What:** Check owner JID first (no network). Only call `GetGroupInfo` if `allow_admin_commands` is true and the JID check failed.

**When to use:** Auth for any in-band command from a group sender.

**Example:**
```go
// Source: inbound.go (new handleOwnerCommand method)

func (a *Adapter) handleOwnerCommand(
    evt *events.Message,
    snap *config.Snapshot,
    listener *config.ResolvedListener,
    cmd string,
) {
    senderJID := evt.Info.Sender.ToNonAD().String()

    authorized := false
    for _, ownerJID := range listener.OwnerJIDs {
        if ownerJID == senderJID {
            authorized = true
            break
        }
    }

    if !authorized && listener.AllowAdminCommands {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        groupInfo, err := a.client.GetGroupInfo(ctx, evt.Info.Chat)
        if err != nil {
            slog.Warn("GetGroupInfo failed for admin check",
                "err", err,
                "group_jid", evt.Info.Chat.String(),
            )
        } else {
            for _, p := range groupInfo.Participants {
                if p.JID.ToNonAD().String() == senderJID && (p.IsAdmin || p.IsSuperAdmin) {
                    authorized = true
                    break
                }
            }
        }
    }

    if !authorized {
        slog.Debug("owner command denied",
            "event", "dispatch",
            "reason", "owner_command_denied",
            "sender_jid", senderJID,
            "cmd", cmd,
        )
        return
    }

    ackText := ownercommands.Handle(cmd, a.ks)
    slog.Info("owner command executed",
        "event", "dispatch",
        "reason", "owner_command",
        "sender_jid", senderJID,
        "cmd", cmd,
        "ack", ackText,
    )
    go a.sendCommandAck(evt, ackText)
}
```

### Pattern 3: ownercommands.Handle — Pure Go State Machine

**What:** Thin package that owns the string → kill-switch-action → ack-string mapping. No whatsmeow imports.

**When to use:** Called only by the adapter, only for pre-authorized commands.

**Example:**
```go
// Source: internal/ownercommands/ownercommands.go

package ownercommands

import "github.com/taldoflemis/bot-camomila/internal/killswitch"

// Handle applies the command to the kill switch and returns the ack string.
// It is only called for pre-authorized commands. Unknown commands return an empty string.
func Handle(cmd string, ks *killswitch.Switch) string {
    switch cmd {
    case "!pause":
        ks.Pause()
        return "paused"
    case "!resume":
        ks.Resume()
        return "resumed"
    default:
        return ""
    }
}
```

### Pattern 4: sendCommandAck — Goroutine Reply (No Jitter)

**What:** Reuses the same `ExtendedTextMessage` + `ContextInfo` shape as `sendReply()`, but without the 2–8 s matcher jitter. Must still run in a goroutine.

**When to use:** Sending any ack/confirmation reply from the event handler.

**Example:**
```go
// Source: inbound.go (new method, mirrors sendReply pattern)

func (a *Adapter) sendCommandAck(evt *events.Message, ackText string) {
    msg := &waE2E.Message{
        ExtendedTextMessage: &waE2E.ExtendedTextMessage{
            Text: proto.String(ackText),
            ContextInfo: &waE2E.ContextInfo{
                StanzaID:      proto.String(evt.Info.ID),
                Participant:   proto.String(evt.Info.Sender.String()),
                QuotedMessage: evt.Message,
            },
        },
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    _, err := a.client.SendMessage(ctx, evt.Info.Chat, msg)
    if err != nil {
        slog.Error("failed to send command ack",
            "event", "send_error",
            "msg_id", evt.Info.ID,
            "err", err,
        )
        return
    }
    slog.Info("command ack sent",
        "event", "reply_sent",
        "msg_id", evt.Info.ID,
        "ack", ackText,
    )
}
```

### Pattern 5: Config Type Extension

**What:** Add `AllowAdminCommands bool` to both `ListenerConfig` (parsed YAML) and `ResolvedListener` (snapshot). YAML zero-value for `bool` is `false`, so omitting the field is safe for existing configs.

**When to use:** Any optional boolean config field that should default to `false`.

**Example:**
```go
// Source: internal/config/config.go

type ListenerConfig struct {
    GroupJID             string   `yaml:"group_jid"`
    OwnerJIDs            []string `yaml:"owner_jids"`
    AllowAdminCommands   bool     `yaml:"allow_admin_commands"`
    Matchers             []string `yaml:"matchers"`
}

type ResolvedListener struct {
    GroupJID             string
    OwnerJIDs            []string
    AllowAdminCommands   bool
    Matchers             []ResolvedMatcher
}
```

In `load.go` `validate()`, propagate the value when building `ResolvedListener`:
```go
resolvedListeners = append(resolvedListeners, ResolvedListener{
    GroupJID:           l.GroupJID,
    OwnerJIDs:          l.OwnerJIDs,
    AllowAdminCommands: l.AllowAdminCommands,
    Matchers:           listenerMatchers,
})
```

### Anti-Patterns to Avoid

- **Calling GetGroupInfo on every message:** Network overhead is unnecessary. Admin check only runs when (a) `allow_admin_commands: true` AND (b) sender not in `owner_jids`. Fast path requires zero network calls.
- **Putting command logic inside the pipeline:** Commands must short-circuit before `a.pipeline.Handle()`; otherwise `!resume` is blocked by the kill switch gate inside the pipeline when the bot is paused.
- **Calling `Disconnect()` from inside the event handler:** Existing codebase invariant. The `sendCommandAck` goroutine is safe because it calls `SendMessage`, not `Disconnect`.
- **Comparing JIDs without `.ToNonAD()`:** AD (agent-device) JIDs include device suffix and will not match stored JID strings. Always call `.ToNonAD().String()` before comparison. This applies to both the owner JID check and the participant scan in `GetGroupInfo`.
- **Re-loading config snapshot inside the command handler:** The handler receives `snap` from the single `a.cfg.Get()` call at the top of `handleMessage()`. Do not call `a.cfg.Get()` again mid-function — violates CONFIG-03.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Atomic pause/resume state | Custom mutex-guarded bool or channel | `killswitch.Switch` (`sync/atomic.Bool`) | Already implemented in `internal/killswitch/`; concurrent-safe, zero-value-false, three-method API. |
| Threaded group reply | Custom proto message builder | `sendReply()` / `sendCommandAck()` pattern (already in codebase) | `ExtendedTextMessage` + `ContextInfo` is the established reply shape for this project. Replicate, do not reinvent. |
| WhatsApp admin check | Parse raw group XML | `client.GetGroupInfo(ctx, jid)` → iterate `GroupParticipant.IsAdmin / IsSuperAdmin` | whatsmeow already parses the group info response and exposes typed fields. |
| JID normalization | Manual string trimming | `.ToNonAD().String()` | whatsmeow JIDs may carry agent/device suffixes. The project-wide invariant is always `.ToNonAD()`. |

---

## whatsmeow API Reference (Verified)

### `client.GetGroupInfo`

```go
// Source: go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69/group.go:591
func (cli *Client) GetGroupInfo(ctx context.Context, jid types.JID) (*types.GroupInfo, error)
```

- **Signature:** takes `context.Context` and `types.JID`; returns `*types.GroupInfo` and `error`. [VERIFIED: module cache grep]
- **Network:** makes a request to WhatsApp servers; must have an active connection. Wrap in `context.WithTimeout`.
- **Group JID:** pass `evt.Info.Chat` directly (it is already a group JID at this point in the gate chain — Gate 1 passed).

### `types.GroupParticipant` fields relevant to admin check

```go
// Source: go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69/types/group.go:103
type GroupParticipant struct {
    JID         JID  // primary JID — use .ToNonAD().String() for comparison
    PhoneNumber JID
    LID         JID
    IsAdmin      bool
    IsSuperAdmin bool
    // ...
}
```

[VERIFIED: module cache read of types/group.go]

- Admin condition: `p.IsAdmin || p.IsSuperAdmin`
- JID comparison: `p.JID.ToNonAD().String() == senderJID` (where senderJID is `evt.Info.Sender.ToNonAD().String()`)

### `types.JID.ToNonAD()`

```go
// Source: go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69/types/jid.go:98
func (jid JID) ToNonAD() JID {
    return JID{User: jid.User, Server: jid.Server, Integrator: jid.Integrator}
}
```

Strips the Agent and Device fields. Project-wide mandatory for any JID comparison. [VERIFIED: module cache read of types/jid.go]

---

## Common Pitfalls

### Pitfall 1: `!resume` blocked when bot is paused

**What goes wrong:** Command check placed after `a.pipeline.Handle()` call, or command check placed inside the pipeline itself. `IsPaused()` returns true, pipeline returns `DropReason: "kill_switch"`, `!resume` never reaches the handler.

**Why it happens:** Developer adds command check as a new pipeline gate rather than a pre-pipeline adapter short-circuit.

**How to avoid:** Command short-circuit must be inserted in `handleMessage()` between Gate 3 (text filter) and the `a.pipeline.Handle()` call (line 122 of current `inbound.go`). Verified: kill switch gate is inside `pipeline.Handle()` at line 58 of `pipeline.go`, not in the adapter.

**Warning signs:** Manual test: set `!pause`, then send `!resume` from owner JID — if bot remains paused, the ordering is wrong.

### Pitfall 2: JID comparison fails due to AD suffix

**What goes wrong:** `evt.Info.Sender.String()` returns an AD JID like `1234567890:0@s.whatsapp.net`; the config stores `1234567890@s.whatsapp.net`. String equality check fails silently and all owners are denied.

**Why it happens:** whatsmeow event JIDs may include a device suffix (multi-device). Config-stored JIDs never have the suffix.

**How to avoid:** Always use `evt.Info.Sender.ToNonAD().String()` for `senderJID`. Also apply `.ToNonAD().String()` to `p.JID` in the `GroupParticipant` loop. The existing codebase already does this in `pipeline.Message` construction (line 112 of `inbound.go`).

**Warning signs:** Owner sends `!pause` and gets no ack; debug log shows `owner_command_denied` for a JID that should be authorized.

### Pitfall 3: `allow_admin_commands` breaks existing configs

**What goes wrong:** Adding `AllowAdminCommands bool` to `ListenerConfig` with `yaml.DisallowUnknownField()` enabled — but this is the parser being strict about fields the config *provides*, not fields with zero values. YAML omission of a bool field results in Go zero value (`false`), which is the intended default.

**Why it happens:** Confusion about strict YAML decoding direction. `DisallowUnknownField()` rejects keys present in YAML that are not in the struct — it does not complain about struct fields absent from YAML.

**How to avoid:** Adding the field to `ListenerConfig` is backward-compatible. Existing configs that omit `allow_admin_commands` will get `false`. No migration needed.

**Warning signs:** Not applicable — this is a non-issue once the field is in the struct. No warning sign to watch for.

### Pitfall 4: Calling GetGroupInfo when bot is not connected

**What goes wrong:** Network call happens during a brief whatsmeow reconnect window, returns error. The command is silently dropped instead of being queued.

**Why it happens:** Network calls from inside event handlers run in the same goroutine as message handling; if the client is reconnecting, the call may fail.

**How to avoid:** Wrap `GetGroupInfo` in `context.WithTimeout(ctx, 10*time.Second)`. On error, log WARN and treat as unauthorized (fail-closed). Do not retry — the operator can re-send the command.

**Warning signs:** `GetGroupInfo failed for admin check` WARN log appearing intermittently under normal operating conditions.

### Pitfall 5: schema.json `additionalProperties: false` rejects new field

**What goes wrong:** `config.schema.json` has `"additionalProperties": false` on the listener object. If `allow_admin_commands` is added to the struct but not to the schema, the JSON schema validator (e.g., in editor tooling) will flag it — though the bot itself uses `go-yaml`, not JSON Schema.

**Why it happens:** Schema and struct drift.

**How to avoid:** Add `"allow_admin_commands": { "type": "boolean", "default": false }` to the listener properties in `config.schema.json` as part of the config extension plan.

---

## Runtime State Inventory

Step 2.5 SKIPPED — this is a greenfield feature addition (new code paths, new config field), not a rename/refactor/migration phase. No stored data, live service config, OS-registered state, secrets, or build artifacts carry a string that needs updating.

---

## Environment Availability

Step 2.6: No new external tools, services, or runtimes required. All dependencies are already in `go.mod` and available. Phase is purely Go source code changes.

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| OWNER-02 originally required DM-only commands | User decision D-07: commands from configured group | 2026-05-24 CONTEXT.md | No DM detection code needed; listener from Gate 1 provides group context. |
| `whatsappadapter.New(cfgStore, pipe)` | `whatsappadapter.New(cfgStore, pipe, ks)` | This phase | `Adapter` struct gains `ks *killswitch.Switch` field. |

**Deprecated/outdated:**
- OWNER-02 (DM-only constraint): Superseded by D-07. The requirement text in REQUIREMENTS.md says "DM only" but the locked decision overrides this. Planner should note the override in the plan.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `context.WithTimeout(ctx, 10*time.Second)` is an appropriate timeout for `GetGroupInfo` over a live WhatsApp connection | Pattern 2 code example | If WhatsApp latency regularly exceeds 10 s, admin-path commands will fail closed; operator re-sends. Low impact. |

---

## Open Questions (RESOLVED)

1. **LID-based participant JIDs in GetGroupInfo** — RESOLVED: Dual-JID comparison implemented in Plan 02 Task 2 `handleOwnerCommand()` admin loop.
   - What we know: `GroupParticipant.JID` is documented as "always equals either the LID or phone number" (verified in `types/group.go`). The existing codebase handles LID vs phone number for mention detection via `botJID` and `botLID` fields.
   - What's unclear: In practice, when a new WhatsApp client sends `evt.Info.Sender`, is it always the phone-number JID after `.ToNonAD()`, or could it be a LID? If it is a LID, comparing against `GroupParticipant.JID` (which may be phone-based in GetGroupInfo) could fail.
   - Resolution: Compare both `p.JID.ToNonAD().String()` and `p.LID.ToNonAD().String()` (when `p.LID` is not empty) in the admin loop — mirrors the dual-JID pattern from `botJID`/`botLID` in `client.go`.

---

## Sources

### Primary (HIGH confidence)
- `internal/killswitch/killswitch.go` — verified `Switch` API: `New()`, `Pause()`, `Resume()`, `IsPaused()`. [VERIFIED: codebase read]
- `internal/whatsappadapter/inbound.go` — verified `sendReply()` pattern, gate ordering in `handleMessage()`. [VERIFIED: codebase read]
- `internal/whatsappadapter/client.go` — verified `Adapter` struct fields and `New()` current signature. [VERIFIED: codebase read]
- `internal/app/app.go` — verified `ks` allocation site and pipeline wiring. [VERIFIED: codebase read]
- `internal/config/config.go` — verified `ListenerConfig` and `ResolvedListener` types. [VERIFIED: codebase read]
- `internal/config/load.go` — verified `validate()` function, listener resolution loop. [VERIFIED: codebase read]
- `internal/pipeline/pipeline.go` — verified kill switch gate is inside pipeline (line 58), not adapter. [VERIFIED: codebase read]
- `go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69/group.go` — verified `GetGroupInfo(ctx, jid)` signature. [VERIFIED: module cache]
- `go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69/types/group.go` — verified `GroupParticipant` fields `IsAdmin`, `IsSuperAdmin`, `JID`. [VERIFIED: module cache]
- `go.mau.fi/whatsmeow@v0.0.0-20260516102357-8d3700152a69/types/jid.go` — verified `ToNonAD()` implementation. [VERIFIED: module cache]
- `config.schema.json` — verified `additionalProperties: false` on listener object; schema update required. [VERIFIED: codebase read]

### Secondary (MEDIUM confidence)
- None — all claims are verified from source code or module cache.

### Tertiary (LOW confidence)
- None.

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries verified from go.mod and module cache
- Architecture: HIGH — all insertion points verified in source code; no speculative claims
- whatsmeow API: HIGH — verified from actual module cache at pinned version
- Pitfalls: HIGH — derived from verified code invariants (JID normalization, gate ordering)

**Research date:** 2026-05-24
**Valid until:** 2026-06-24 (whatsmeow is a pseudo-version; API stable within this pinned commit)
