# Phase 3: Owner Commands & Operability - Context

**Gathered:** 2026-05-24
**Status:** Ready for planning

<domain>
## Phase Boundary

Owner can pause and resume the bot by sending `!pause` / `!resume` in the configured group chat — no process restart. Auth is enforced: only senders in `owner_jids` (per-listener) can issue commands, with an opt-in to also allow group admins/super-admins. Kill switch state lives outside the config snapshot and is never reset by hot-reload.

**Requirements change from spec:** OWNER-02 originally said "DM only, never in group". User decision: commands trigger from the group, not DMs. DM detection and routing are NOT part of this phase.

</domain>

<decisions>
## Implementation Decisions

### Kill Switch Wire-up
- **D-01:** Pass `*killswitch.Switch` directly to `whatsappadapter.New()` alongside the pipeline: `whatsappadapter.New(cfgStore, pipe, ks)`. Adapter holds both `*pipeline.Pipeline` and `*killswitch.Switch`. Owner command handler calls `ks.Pause()` / `ks.Resume()` directly. No indirection through the pipeline.
- **D-02:** `app.go` already creates `ks := killswitch.New()` and passes it to `pipeline.New(ks, ...)`. The only change is also passing `ks` to `whatsappadapter.New()`.

### Owner Auth
- **D-03:** `owner_jids` stays in `ListenerConfig` (per-listener, no schema promotion). No global owner list. Command auth: check sender JID (`.ToNonAD()`) against `listener.OwnerJIDs`. Authorized if match found.
- **D-04:** New optional field `allow_admin_commands: bool` in `ListenerConfig` (default `false`). When `true`, group admins (`IsAdmin == true OR IsSuperAdmin == true` from whatsmeow `GetGroupInfo`) may also issue commands, in addition to owner JIDs.
- **D-05:** If `allow_admin_commands: true`, adapter calls `client.GetGroupInfo(groupJID)` to resolve sender role. This is a network call — acceptable for a rarely-triggered command path.
- **D-06:** Auth check order: (1) sender JID in `listener.OwnerJIDs` → authorized immediately, no network call. (2) if `allow_admin_commands: true` → call GetGroupInfo, check IsAdmin/IsSuperAdmin. Non-authorized senders are silently ignored (no ack, no log at WARN — debug only).

### Command Routing
- **D-07:** Commands trigger from the configured group, NOT DMs. Owner JID lookup has group context (the listener), so no DM routing or DM detection is needed.
- **D-08:** Command check happens **before the pipeline** in `handleMessage`, after gates 0–3 (history sync, scope, self-message, text-only). If text matches `!pause` or `!resume` (case-insensitive, trimmed), run auth check and dispatch — skip the matcher pipeline entirely. Commands never enter the pipeline.
- **D-09:** Command parsing: `strings.TrimSpace(strings.ToLower(text))` == `"!pause"` or `"!resume"`. Exact match after trim+lowercase only. No partial or prefix matching.

### Ack Reply
- **D-10:** Ack is a threaded reply to the command message in the group (same pattern as matcher replies: `ExtendedTextMessage` with `ContextInfo`). Reply text: `"paused"` for `!pause`, `"resumed"` for `!resume`. Brief, unambiguous.
- **D-11:** Ack is sent with the same goroutine+jitter pattern as matcher replies (`go a.sendCommandAck(evt, ackText)`). No jitter needed for command acks — but using a goroutine is mandatory to avoid blocking the event handler.

### ownercommands Package
- **D-12:** Per CLAUDE.md architecture, `internal/ownercommands/` is a pure Go package (no whatsmeow imports). Its interface: receives parsed command string + `*killswitch.Switch` + returns ack string. The adapter handles all whatsmeow interaction (admin lookup, sending ack). The package can be thin — primarily exists to keep command dispatch logic testable outside the adapter.
- **D-13:** The adapter pre-resolves auth (owner JID check + optional admin check) and only calls `ownercommands.Handle(cmd, ks)` when authorized. The ownercommands package never sees unauthorized calls.

### Claude's Discretion
- Logging: every command attempt (authorized and silently dropped) should be logged with `reason` field per CLAUDE.md logging invariants: `owner_command` for authorized execution, `owner_command_denied` for unauthorized (debug level only).
- The `allow_admin_commands` field defaults to `false` and must not affect existing configs that omit it (YAML zero-value safe).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements
- `.planning/REQUIREMENTS.md` §OWNER-01 – OWNER-06 — full owner command spec (note: OWNER-02 DM constraint overridden by D-07 above)
- `.planning/REQUIREMENTS.md` §OBSERV-02 — every match/drop decision logged with `reason` field (applies to command dispatch too)

### Architecture
- `CLAUDE.md` §Architecture — `ownercommands/` package placement, hexagonal boundary (whatsappadapter is ONLY whatsmeow importer)
- `CLAUDE.md` §Critical Invariants — kill switch outside config snapshot, JID normalization with `.ToNonAD()`

### Existing Code
- `internal/killswitch/killswitch.go` — `Switch` type, `Pause()`, `Resume()`, `IsPaused()` — interface for ownercommands
- `internal/app/app.go` — composition root where `ks` is created and wired; `whatsappadapter.New()` signature changes here
- `internal/whatsappadapter/client.go` — `Adapter` struct + `New()` signature to extend with `*killswitch.Switch`
- `internal/whatsappadapter/inbound.go` — `handleMessage()` where command short-circuit (D-08) is inserted; `sendReply()` pattern to replicate for command acks
- `internal/config/config.go` — `ListenerConfig` struct where `allow_admin_commands: bool` is added

### Config
- `config.example.yaml` — YAML shape; must be updated to show `allow_admin_commands: false` in listener example

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/whatsappadapter/inbound.go:sendReply()` — threaded reply pattern (`ExtendedTextMessage` + `ContextInfo`); reuse for command acks
- `internal/killswitch/killswitch.go` — `Switch.Pause()` / `Switch.Resume()` already implement the state change; ownercommands just calls them
- `internal/whatsappadapter/inbound.go:handleMessage()` — gates 0–3 already in place; command short-circuit inserts after gate 3

### Established Patterns
- JID comparison uses `.ToNonAD().String()` — mandatory for owner JID matching (CLAUDE.md invariant)
- Goroutine for all sends from event handler — never block `onEvent`/`handleMessage`
- Single atomic `snap := a.cfg.Get()` load held for full call duration (CONFIG-03)

### Integration Points
- `whatsappadapter.New(cfgStore, pipe)` → `whatsappadapter.New(cfgStore, pipe, ks)` — only signature change needed
- `app.go` passes `ks` to both `pipeline.New()` (existing) and `whatsappadapter.New()` (new)
- `client.GetGroupInfo(groupJID)` — whatsmeow call for admin check; only invoked when `allow_admin_commands: true`

</code_context>

<specifics>
## Specific Ideas

- User explicitly moved commands from DM to group to simplify routing and avoid DM detection complexity
- "Bot owner" (config JID) vs "group admin" (WhatsApp role) are separate auth tiers — bot owner is always authorized, group admin is opt-in per listener
- Ack message text verbatim: `"paused"` / `"resumed"` (from REQUIREMENTS.md OWNER-05)

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 3-Owner Commands & Operability*
*Context gathered: 2026-05-24*
