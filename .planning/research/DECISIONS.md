---
updated: "2026-05-23T11:47:00Z"
---

# Architecture Decision Records

## ADR-001: Hexagonal Architecture with whatsmeow Firewall

**Date:** 2026-05-22
**Status:** Accepted
**Context:** Need to isolate WhatsApp protocol dependency for testability.
**Decision:** Only `internal/whatsappadapter` imports whatsmeow. All other packages are pure Go.
**Consequences:** Domain and matcher logic is fully unit-testable without a WhatsApp connection.

## ADR-002: Config Hot-Reload via Atomic Pointer

**Date:** 2026-05-23
**Status:** Accepted
**Context:** Need lock-free config access on the hot path.
**Decision:** Use `atomic.Pointer[Snapshot]` with fsnotify watcher on parent directory.
**Consequences:** Zero contention on reads; watcher debounces at 200ms; mtime poll fallback at 30s.

## ADR-003: Kill Switch Outside Config Snapshot

**Date:** 2026-05-22
**Status:** Accepted
**Context:** Hot-reload must never reset the kill switch state.
**Decision:** Kill switch will be an `atomic.Bool` in a `Runtime` struct, separate from config.
**Consequences:** Owner !pause/!resume commands survive config reloads.

---

## Phase 2 Decisions

**Date:** 2026-05-23

### Scope
- Phase 2 ships match + reply + cooldown + quiet hours as one atomic bundle — no partial deploys
- Kill switch gate wired in Phase 2 as a no-op placeholder (`internal/killswitch` with `atomic.Bool`, always-open). Phase 3 only adds the DM command parser.
- Unit tests included for all new pure-Go packages (matcher, cooldown, quiet hours, pipeline)

### Architecture — Matcher Pipeline
- Chose: Pure-Go `internal/matcher` package with `Pipeline.Handle(domain.Message) *MatchResult`
- Reason: WhatsApp adapter constructs `domain.Message` and calls pipeline — keeps adapter thin, fully testable without whatsmeow

### Architecture — Cooldown State
- Chose: Separate `internal/cooldown` package with `sync.Map` keyed by `(matcher, senderJID)` + background reaper goroutine
- Reason: Injectable clock for testing; reaper lifecycle tied to context; cleanly separated from matcher logic

### Architecture — Quiet Hours
- Chose: Separate `internal/quiethours` package with pure function `Check(now, loc, cfg) bool`
- Reason: Trivially testable with time fixtures including midnight wrap-around and DST edge cases

### Architecture — Kill Switch
- Chose: `internal/killswitch` package with `atomic.Bool` wrapper, wired as first gate in Phase 2
- Reason: Phase 3 only needs to add the command parser; pipeline structure is stable from Phase 2 forward

### Reply Jitter
- Chose: After match+cooldown decision, sleep the jitter in a goroutine, then send
- Reason: Event handler returns immediately so whatsmeow's dispatch lock isn't held (deadlock prevention per Pitfall 6)

### Variable Substitution
- `{REPLIED_USER}` resolves to `PushName` only (empty string if unavailable)
- Reason: Simpler; avoids exposing phone numbers in group messages

### Levenshtein Library
- Chose: `agnivade/levenshtein` (~330ns/op, rune-safe, well-maintained)
- Reason: Lightweight, handles Unicode correctly, widely used

### Self-Loop Prevention
- Skip quoted-text matching when the quoted message author is the bot itself (quote-chain loop prevention)
- Keep the existing startup-time answer/keyword overlap check (CONFIG-02 self-loop guard)

### Testing
- Chose: Unit tests for matcher, cooldown, quiet hours, pipeline using testify
- Reason: These are pure-Go packages with zero whatsmeow dependency — no excuse not to test

