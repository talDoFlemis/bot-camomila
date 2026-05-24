---
phase: 03-owner-commands-operability
verified: 2026-05-24T00:00:00Z
status: human_needed
score: 9/9 must-haves verified
overrides_applied: 1
overrides:
  - must_have: "Owner commands are accepted only via DM to the bot, never in the configured group (OWNER-02)"
    reason: "User decision D-07 captured in 03-CONTEXT.md and ROADMAP.md Note: commands trigger from the configured group, not DMs. OWNER-02 DM constraint superseded before any implementation. Inbound gate correctly uses listener scope (group) — no DM routing needed."
    accepted_by: "user (captured in 03-CONTEXT.md D-07)"
    accepted_at: "2026-05-24T00:00:00Z"
human_verification:
  - test: "Send !pause from an authorized owner JID in the configured group"
    expected: "Bot replies with threaded 'paused' ack; subsequent group trigger messages are silently dropped (reason: kill_switch in logs)"
    why_human: "Requires a live WhatsApp connection and a real group; cannot verify network I/O or message delivery in CI"
  - test: "Send !resume from same authorized JID while bot is paused"
    expected: "Bot replies with threaded 'resumed' ack; subsequent trigger messages resume normal handling"
    why_human: "State-flip across pause/resume cycle requires live WhatsApp session to confirm round-trip"
  - test: "Send !pause from a non-owner JID (someone not in owner_jids and not a group admin)"
    expected: "Bot is silently unresponsive; debug log line with reason:owner_command_denied appears; kill switch state unchanged"
    why_human: "Requires live group with multiple participants; cannot exercise WhatsApp participant lookup in CI"
  - test: "Hot-reload config (edit config.yaml) while bot is paused"
    expected: "Kill switch remains paused after reload; bot does not suddenly resume"
    why_human: "Requires a running process, config write, and observing atomic state across hot-reload boundary"
---

# Phase 3: Owner Commands & Operability Verification Report

**Phase Goal:** Implement owner commands (!pause / !resume) so an authorized WhatsApp group member can pause and resume the bot from the chat without a process restart.
**Verified:** 2026-05-24
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `ownercommands.Handle("!pause", ks)` calls `ks.Pause()` and returns `"paused"` | VERIFIED | `ownercommands.go:16-19` — `case "!pause": ks.Pause(); return "paused"`. TestHandle case passes (4/4). |
| 2 | `ownercommands.Handle("!resume", ks)` calls `ks.Resume()` and returns `"resumed"` | VERIFIED | `ownercommands.go:20-23` — `case "!resume": ks.Resume(); return "resumed"`. TestHandle case passes. |
| 3 | Unknown command returns empty string without modifying kill switch | VERIFIED | `ownercommands.go:24-26` — `default: return ""`. TestHandle cases for `"!unknown"` and `""` pass. |
| 4 | `AllowAdminCommands` bool exists in both `ListenerConfig` and `ResolvedListener`; `validate()` propagates it | VERIFIED | `config.go:52` and `config.go:104` — fields present. `load.go:164` — `AllowAdminCommands: l.AllowAdminCommands` in struct literal. `grep -c` returns 2 in config.go, 1 in load.go. |
| 5 | Command short-circuit fires after Gate 3, before `pipeline.Handle()` — `!resume` works even when kill switch is active | VERIFIED | `inbound.go:111-115` — normalized check at line 112; `pipeline.Handle` at line 132. Short-circuit at line 112 < pipeline at line 132. Pipeline itself drops kill_switch messages (`pipeline.go:58-59`), so the pre-pipeline short-circuit is mandatory and present. |
| 6 | Owner JID check uses `.ToNonAD().String()`; admin LID check compares both `p.JID` and `p.LID` when non-empty | VERIFIED | `inbound.go:165` — `senderJID := evt.Info.Sender.ToNonAD().String()`. `inbound.go:193` — `p.JID.ToNonAD().String() == senderJID`. `inbound.go:198` — `!p.LID.IsEmpty() && p.LID.ToNonAD().String() == senderJID`. |
| 7 | Unauthorized senders silently dropped at debug level with `reason: owner_command_denied` | VERIFIED | `inbound.go:207-213` — `slog.Debug("owner command denied", ... "reason", "owner_command_denied", ...)`. No ack sent. |
| 8 | Same `*killswitch.Switch` instance passed to both `pipeline.New()` and `whatsappadapter.New()` — hot-reload never resets it | VERIFIED | `app.go:52` — `ks := killswitch.New()`. `app.go:55` — `pipe := pipeline.New(ks, ...)`. `app.go:68` — `whatsappadapter.New(cfgStore, pipe, ks)`. Config watcher (`watcher.go`, `store.go`) has zero reference to killswitch. |
| 9 | `ownercommands` package is pure Go with no whatsmeow imports (hexagonal boundary) | VERIFIED | `grep -r "whatsmeow\|waE2E\|events\." internal/ownercommands/` returns zero matches. Single import: `internal/killswitch`. |

**Score:** 9/9 truths verified (plus OWNER-02 override — see below)

### OWNER-02 Deviation

**Requirement text:** "Owner commands are accepted only via DM to the bot, never in the configured group."

**Actual implementation:** Commands trigger from the configured group. The adapter's `handleMessage()` uses the group listener gate (Gate 1) so only the configured group is in scope — the command path has listener context by design.

**Override basis:** User decision D-07, captured in `03-CONTEXT.md` before any implementation and echoed in ROADMAP.md Phase 3 Note. The ROADMAP acceptance criteria were written against group-based commands. This is not a regression — it is a deliberate, pre-approved requirement change that strictly subsumes the original capability (group auth is more visible and auditable than DM routing).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/ownercommands/ownercommands.go` | `Handle(cmd, ks)` pure Go dispatch | VERIFIED | 27 lines, single import `internal/killswitch`, no whatsmeow |
| `internal/ownercommands/ownercommands_test.go` | Table-driven `TestHandle` | VERIFIED | 4 cases: `!pause`, `!resume`, unknown, empty — all pass |
| `internal/config/config.go` | `AllowAdminCommands` in `ListenerConfig` + `ResolvedListener` | VERIFIED | Lines 52 and 104 — both fields present with YAML tags |
| `internal/config/load.go` | `validate()` propagates `AllowAdminCommands` | VERIFIED | Line 164 — `AllowAdminCommands: l.AllowAdminCommands` in struct literal |
| `internal/whatsappadapter/client.go` | `Adapter.ks` field; `New()` 3-arg signature | VERIFIED | Line 37 — `ks *killswitch.Switch`; line 47 — 3-arg `New()` |
| `internal/whatsappadapter/inbound.go` | `handleOwnerCommand()` + `sendCommandAck()` + short-circuit | VERIFIED | All three present: short-circuit line 112, `handleOwnerCommand` line 164, `sendCommandAck` line 230 |
| `internal/app/app.go` | `whatsappadapter.New(cfgStore, pipe, ks)` | VERIFIED | Line 68 — exact 3-arg call; old 2-arg call absent |
| `config.schema.json` | `allow_admin_commands` property in listener items | VERIFIED | Line 33 — property present |
| `config.example.yaml` | `allow_admin_commands: false` in each listener | VERIFIED | Lines 9, 16 — both listener blocks updated with comment |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/ownercommands/ownercommands.go` | `internal/killswitch/killswitch.go` | `import github.com/taldoflemis/bot-camomila/internal/killswitch` | WIRED | Only import in file; `*killswitch.Switch` used as parameter type |
| `internal/config/load.go` | `internal/config/config.go` | `AllowAdminCommands: l.AllowAdminCommands` in `validate()` | WIRED | Line 164 — field propagated in `resolvedListeners` append |
| `internal/app/app.go` | `internal/whatsappadapter/client.go` | `whatsappadapter.New(cfgStore, pipe, ks)` | WIRED | Line 68 — confirmed by grep |
| `internal/whatsappadapter/inbound.go` | `internal/ownercommands/ownercommands.go` | `ownercommands.Handle(cmd, a.ks)` | WIRED | Line 216 — call inside `handleOwnerCommand()` after auth passes |
| `internal/whatsappadapter/inbound.go` | `internal/killswitch/killswitch.go` | `a.ks` field set in `New()` | WIRED | `a.ks` accessed at line 216; field set via `New()`'s `ks: ks` |

### Data-Flow Trace (Level 4)

Not applicable — this phase adds command dispatch logic (event-driven state mutation), not data-rendering components. The kill switch is a `sync/atomic.Bool` — its state change is the data flow. Level 4 trace is satisfied by the test evidence: `TestHandle/pause_command_pauses_bot_and_returns_paused` verifies `ks.IsPaused() == true` after `Handle("!pause", ks)`.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `go build ./...` exits 0 | `go build ./...` | exit 0 | PASS |
| All tests pass | `go test ./...` | 6 packages tested, 0 failures | PASS |
| `go vet ./...` exits 0 | `go vet ./...` | exit 0 | PASS |
| ownercommands TestHandle 4 cases | `go test ./internal/ownercommands/... -v` | PASS: 4/4 cases | PASS |
| config AllowAdminCommands tests | `go test ./internal/config/... -v` | PASS: TestAllowAdminCommandsZeroValue, TestAllowAdminCommandsTrue | PASS |
| No whatsmeow imports in ownercommands | `grep -r "whatsmeow" internal/ownercommands/` | 0 matches | PASS |

### Probe Execution

No probes declared in PLAN frontmatter or SUMMARY. No `scripts/*/tests/probe-*.sh` files found. Step 7c: SKIPPED (no probes declared).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|---------|
| OWNER-01 | 03-01, 03-02 | Hardcoded owner JID list in YAML config; only those JIDs may issue commands | SATISFIED | `ListenerConfig.OwnerJIDs` parsed by `validate()`; `handleOwnerCommand()` checks sender against it |
| OWNER-02 | 03-02 | Commands via DM only — **superseded by D-07** | PASSED (override) | User decision: commands from configured group. ROADMAP Note and `03-CONTEXT.md` both document this. Implementation correctly uses group listener context |
| OWNER-03 | 03-01, 03-02 | `!pause` flips `atomic.Bool` kill switch; `!resume` clears it | SATISFIED | `ownercommands.Handle()` + `killswitch.Switch.Pause()/Resume()` — verified by TestHandle |
| OWNER-04 | 03-02 | Kill switch drops group messages at first pipeline gate; owner commands still reach handler | SATISFIED | `pipeline.go:58-59` — kill_switch gate. `inbound.go:111-115` — command short-circuit BEFORE pipeline call at line 132 |
| OWNER-05 | 03-02 | Bot acks owner command with brief reply ("paused" / "resumed") | SATISFIED | `sendCommandAck()` sends `ExtendedTextMessage` + `ContextInfo` threaded reply with `ackText` ("paused"/"resumed") from `ownercommands.Handle()` |
| OWNER-06 | 03-01, 03-02 | Kill switch lives outside config snapshot; hot-reload does not reset it | SATISFIED | `ks := killswitch.New()` in `app.go`; same pointer passed to both pipeline and adapter; config watcher has zero reference to killswitch |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | No debt markers (TBD/FIXME/XXX/TODO) found in any phase-modified file | — | — |
| — | — | No stub patterns (empty returns, placeholder values) found | — | — |

Anti-pattern scan of all 7 files modified by this phase returned no hits. All implementations are substantive.

### Human Verification Required

#### 1. !pause / !resume end-to-end in live group

**Test:** Connect the bot to a real WhatsApp group. From an authorized owner JID, send `!pause` in the configured group.
**Expected:** Bot sends a threaded reply `"paused"` to the command message. Bot logs `reason: owner_command` at INFO. Subsequent trigger messages (matching words from config) are dropped with `reason: kill_switch` in logs. Then send `!resume` — bot replies `"resumed"` and resumes normal matching.
**Why human:** Requires a live WhatsApp multi-device session and network connectivity to WhatsApp servers. Cannot be exercised in CI.

#### 2. Unauthorized sender silently ignored

**Test:** Send `!pause` from a JID that is NOT in `owner_jids` and is NOT a group admin.
**Expected:** Bot sends no reply. Debug log shows `reason: owner_command_denied`. Kill switch state unchanged.
**Why human:** Requires a real WhatsApp group with multiple participants and a second device to verify silence.

#### 3. Hot-reload does not reset kill switch

**Test:** Pause the bot (`!pause`). Then edit `config.yaml` (e.g., change a cooldown value). Observe bot logs for the config reload. Then send a trigger message.
**Expected:** Bot remains paused after reload — trigger message dropped with `reason: kill_switch`. Kill switch state is not reset.
**Why human:** Requires observing atomic state across a live config-reload event with filesystem watch active.

#### 4. allow_admin_commands admin path (optional — only if config uses it)

**Test:** Set `allow_admin_commands: true` in config. Send `!pause` from a group admin JID that is NOT in `owner_jids`.
**Expected:** Bot calls `GetGroupInfo`, confirms admin role, executes command, replies `"paused"`.
**Why human:** Requires a live group where the test sender has WhatsApp group admin role; cannot mock `GetGroupInfo` network call in CI.

### Gaps Summary

No automated gaps found. All 9 must-haves are verified. The OWNER-02 deviation is an accepted override (pre-approved user decision captured in ROADMAP.md and context doc before implementation began).

Four items require live human testing because they involve real WhatsApp network I/O — these are the standard operability checks for any WhatsApp bot feature.

---

_Verified: 2026-05-24_
_Verifier: Claude (gsd-verifier)_
