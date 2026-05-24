---
milestone: v1.0
version: 1.0.0
updated: "2026-05-24T00:00:00Z"
---

# Roadmap

> **Current Phase:** 3 - Owner Commands & Operability
> **Status:** not started

## Must-Haves (from SPEC)

- [x] Fuzzy-match keywords and reply with calming answers (Phase 2)
- [x] Cooldowns, quiet hours, and rate limiting (Phase 2)
- [ ] Owner kill switch via group command (Phase 3)
- [ ] Docker packaging for VPS deployment (Phase 4)

---

## Phases

### Phase 1: Session & Config Foundations

**Status:** ✅ Complete
**Objective:** Bot connects to WhatsApp, authenticates via QR, persists its session, gates the configured group, and logs all lifecycle events — with no matching or replies yet.
**Requirements:** SESSION-01, SESSION-02, SESSION-03, SESSION-04, SESSION-05, CONFIG-01, CONFIG-02, CONFIG-03, CONFIG-04, CONFIG-05, SCOPE-01, SCOPE-02, SCOPE-03, OBSERV-01, OBSERV-03

**Plans:**

- [x] Plan 1.1: Module deps + hexagonal directory scaffold + Config/Snapshot types + domain.Message + entrypoint stub
- [x] Plan 1.2: Config package: YAML load/validate + atomic Store + fsnotify Watcher with debounce + mtime fallback
- [x] Plan 1.3: WhatsApp adapter: waLog bridge + SQLite/sqlstore + QR pairing + lifecycle event handler
- [x] Plan 1.4: Entrypoint wiring: app.Run() composition root + graceful shutdown + operator verification

---

### Phase 2: Matcher Pipeline & Safe Dispatch

**Status:** ✅ Complete
**Objective:** Bot detects trigger words in group messages and replies with a calming answer, with cooldowns, quiet hours, rate limiting, and jitter all enforced as a single indivisible bundle.
**Depends on:** Phase 1
**Requirements:** MATCH-01, MATCH-02, MATCH-03, MATCH-04, MATCH-05, REPLY-01, REPLY-02, REPLY-03, REPLY-04, REPLY-05, COOLDOWN-01, COOLDOWN-02, COOLDOWN-03, COOLDOWN-04, QUIET-01, QUIET-02, QUIET-03, OBSERV-02

**Plans:**

- [x] Plan 2.1: Dependencies + domain/config type extensions (wave 1)
- [x] Plan 2.2: Fuzzy matcher engine — Levenshtein, NFC, tokenization (wave 1)
- [x] Plan 2.3: Cooldown engine — per-matcher + per-user with injectable clock (wave 1)
- [x] Plan 2.4: Quiet hours + kill switch gate (wave 1)
- [x] Plan 2.5: Pipeline orchestrator — composes all gates + rate limiter (wave 2)
- [x] Plan 2.6: Adapter integration — threaded reply, jitter, app.Run wiring (wave 3)

---

### Phase 3: Owner Commands & Operability

**Status:** ⬜ Not Started
**Objective:** Operator can pause and resume the bot from the configured group without restarting the process.
**Depends on:** Phase 2
**Requirements:** OWNER-01, OWNER-02, OWNER-03, OWNER-04, OWNER-05, OWNER-06

**Note:** OWNER-02 DM constraint superseded by user decision D-07: commands trigger from the configured group, not DMs.

**Acceptance criteria:**

- `!pause` from an authorized JID in the configured group sets the kill switch; subsequent group messages are dropped and logged.
- `!resume` clears the kill switch; bot acks both commands with a threaded group reply.
- Commands from non-owner JIDs are silently ignored (debug log only).
- Hot-reload does not change the kill switch state.

**Plans:** 1/2 plans executed

Plans:
**Wave 1**

- [x] 03-01-PLAN.md — Config AllowAdminCommands extension + ownercommands package

**Wave 2** *(blocked on Wave 1 completion)*

- [ ] 03-02-PLAN.md — Adapter ks wiring + handleOwnerCommand + sendCommandAck

---

### Phase 4: Docker Packaging & Deploy

**Status:** ⬜ Not Started
**Objective:** Bot ships as a minimal distroless container image ready for VPS deployment.
**Depends on:** Phase 3
**Requirements:** DEPLOY-01, DEPLOY-02, DEPLOY-03, DEPLOY-04, DEPLOY-05

**Acceptance criteria:**

- Multi-stage image with distroless runtime ≤25 MB.
- docker compose starts the bot with mounted volumes; graceful shutdown within 10 s.
- .dockerignore excludes *.sqlite* files.
- README documents SQLite backup procedure.

**Plans:** TBD

---

## Progress Summary

| Phase | Status | Plans | Completed |
|-------|--------|-------|-----------|
| 1. Session & Config Foundations | ✅ | 4/4 | 2026-05-23 |
| 2. Matcher Pipeline & Safe Dispatch | ✅ | 6/6 | 2026-05-23 |
| 3. Owner Commands & Operability | 1/2 | In Progress|  |
| 4. Docker Packaging & Deploy | ⬜ | 0/0 | — |

---

## Timeline

| Phase | Started | Completed | Duration |
|-------|---------|-----------|----------|
| 1 | 2026-05-22 | 2026-05-23 | 1 day |
| 2 | 2026-05-23 | 2026-05-23 | <1 day |
| 3 | — | — | — |
| 4 | — | — | — |
