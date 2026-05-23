---
updated: "2026-05-23T11:30:00Z"
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
