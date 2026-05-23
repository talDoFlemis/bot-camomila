# Roadmap

## Phases

- [ ] **Phase 1 — Session & Config Foundations** - Bot connects, authenticates, gates the group, and logs everything; no matching yet.
- [ ] **Phase 2 — Matcher Pipeline & Safe Dispatch** - Bot detects heated messages and replies with cooldowns and quiet hours enforced.
- [ ] **Phase 3 — Owner Commands & Operability** - Operator can pause and resume the bot via DM without restarting the process.
- [ ] **Phase 4 — Docker Packaging & Deploy** - Bot ships as a container image ready for VPS deployment.

## Phase Details

### Phase 1 — Session & Config Foundations
**Goal**: Bot connects to WhatsApp, authenticates via QR, persists its session, gates the configured group, and logs all lifecycle events — with no matching or replies yet.
**Mode**: mvp
**Depends on**: Nothing (first phase)
**Requirements**: SESSION-01, SESSION-02, SESSION-03, SESSION-04, SESSION-05, CONFIG-01, CONFIG-02, CONFIG-03, CONFIG-04, CONFIG-05, SCOPE-01, SCOPE-02, SCOPE-03, OBSERV-01, OBSERV-03
**Acceptance criteria**:
- Bot prints a QR code on first launch; after scanning, session is persisted in SQLite and bot reconnects on restart without re-pairing.
- On startup, `PRAGMA integrity_check` passes silently; on failure or on `events.LoggedOut`, bot exits non-zero with an ERROR-level log.
- Any message not from the configured group JID, any `IsFromMe` message, and any non-text message type is dropped at the first gate; the drop is logged with structured fields.
- Hot-reload fires within ~500 ms of a YAML save; on invalid config the previous snapshot is retained and a WARN is logged; mtime polling activates if fsnotify fails.
- `SIGTERM` / `SIGINT` initiates clean shutdown of whatsmeow and SQLite within 10 s.
**Risks**:
- whatsmeow pseudo-version pin may drift from the actual Go toolchain 1.26.3 requirement; verify go.mod compatibility before writing any code.
- `modernc.org/sqlite` dialect must be registered as `"sqlite3"` for sqlstore compatibility; a mismatch will silently fail session writes.
- First-pair `HistorySync` flood may fire old messages at the bot; timestamp-filter events to drop any message predating bot start time.
**Plans**: TBD

### Phase 2 — Matcher Pipeline & Safe Dispatch
**Goal**: Bot detects trigger words in group messages and replies with a calming answer, with cooldowns, quiet hours, rate limiting, and jitter all enforced as a single indivisible bundle.
**Mode**: mvp
**Depends on**: Phase 1
**Requirements**: MATCH-01, MATCH-02, MATCH-03, MATCH-04, MATCH-05, REPLY-01, REPLY-02, REPLY-03, REPLY-04, REPLY-05, COOLDOWN-01, COOLDOWN-02, COOLDOWN-03, COOLDOWN-04, QUIET-01, QUIET-02, QUIET-03, OBSERV-02
**Acceptance criteria**:
- A message containing a configured keyword (within Levenshtein distance) in the group chat triggers a threaded WhatsApp reply quoting the original message, after a 2–8 s delay.
- The same matcher does not fire again within its configured cooldown window; the same sender cannot re-trigger the same matcher within the per-user cooldown window.
- During the configured quiet-hours window (including midnight wrap-around), no reply is sent; the suppression is logged with reason.
- When the global rate cap (≤6/min, ≤30/hr) would be exceeded, the reply is dropped and logged; reply drops for all reasons (cooldown, quiet hours, rate cap, kill-switch placeholder) are logged with a structured `reason` field.
- Short words below the per-distance minimum length never produce a false positive match, even when the input contains Unicode tokens that are NFC-normalized and lowercased.
**Risks**:
- Deploying reply capability without cooldowns active is a spam footgun in a live group; match, reply, and cooldown must ship as one unit — no partial deploys.
- Per-token Levenshtein on Portuguese text may produce unexpected false positives for short common words; validate distance:1 threshold on real group message samples before enabling in production.
- `ContextInfo{StanzaId, Participant, QuotedMessage}` construction for threaded replies is not well-documented; verify against whatsmeow upstream source before implementing.
**Plans**: TBD

### Phase 3 — Owner Commands & Operability
**Goal**: Operator can pause and resume the bot from a phone DM without restarting the process, with the kill switch checked as the first gate in the matcher pipeline.
**Mode**: mvp
**Depends on**: Phase 2
**Requirements**: OWNER-01, OWNER-02, OWNER-03, OWNER-04, OWNER-05, OWNER-06
**Acceptance criteria**:
- Sending `!pause` in a DM from an owner JID sets the kill switch; subsequent group messages are dropped at the first gate and logged; owner DMs continue to process.
- Sending `!resume` in a DM from an owner JID clears the kill switch; the bot acks both commands with a brief DM reply confirming the action.
- Commands sent from a non-owner JID or sent in the group (not a DM) are silently ignored.
- A hot-reload that succeeds or fails does not change the kill switch state.
**Risks**:
- Kill switch state must live in a `Runtime` struct outside the config snapshot; placing it inside `atomic.Pointer[Snapshot]` would reset it on every hot-reload.
- Multi-device JID normalization (`ToNonAD()`) is required when comparing sender JID to the owner allowlist; a mismatch silently breaks the command gate.
**Plans**: TBD

### Phase 4 — Docker Packaging & Deploy
**Goal**: Bot ships as a minimal distroless container image with mounted volumes for session and config, ready to run on a VPS with graceful shutdown and a documented backup procedure.
**Mode**: mvp
**Depends on**: Phase 3
**Requirements**: DEPLOY-01, DEPLOY-02, DEPLOY-03, DEPLOY-04, DEPLOY-05
**Acceptance criteria**:
- `docker build` produces a multi-stage image with a distroless static runtime layer; `docker image inspect` shows the final image at or below 25 MB.
- `docker compose up` starts the bot with SQLite and YAML mounted as volumes; `docker compose stop` triggers graceful shutdown and the bot exits cleanly within 10 s.
- The built image does not contain any `*.sqlite*` files (verified by `.dockerignore`).
- README documents the stop-copy-start backup procedure for the SQLite session DB including `-wal` and `-shm` sidecar files.
**Risks**:
- `distroless/static-debian13:nonroot` requires a fully CGO-free binary; any transitive CGO dependency will cause a runtime crash with no useful error message.
- Named-volume vs bind-mount choice affects SQLite WAL reliability on the target VPS filesystem; document the trade-off and default to bind-mount on ext4.
**Plans**: TBD

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Session & Config Foundations | 0/0 | Not started | - |
| 2. Matcher Pipeline & Safe Dispatch | 0/0 | Not started | - |
| 3. Owner Commands & Operability | 0/0 | Not started | - |
| 4. Docker Packaging & Deploy | 0/0 | Not started | - |
