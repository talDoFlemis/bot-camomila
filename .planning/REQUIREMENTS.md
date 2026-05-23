# bot-camomila — Requirements

Scope: v1 = the minimum bot that can be safely left running in one WhatsApp group. Categories follow the research convergence (Session, Config, Scope, Match, Reply, Cooldown, Quiet, Owner, Deploy, Observ).

## v1 Requirements

### Session

- [ ] **SESSION-01**: Bot pairs to WhatsApp via on-terminal QR code on first launch.
- [ ] **SESSION-02**: Bot persists the whatsmeow device session in SQLite (`modernc.org/sqlite`, CGO-free) and resumes automatically across restarts without re-pairing.
- [ ] **SESSION-03**: On startup, bot runs SQLite `PRAGMA integrity_check` and exits non-zero on failure (no silent corruption).
- [ ] **SESSION-04**: On `events.LoggedOut`, bot logs the event loudly at ERROR level and exits non-zero (no auto re-pair).
- [ ] **SESSION-05**: Bot survives transient WhatsApp disconnects (network blips, multi-device reconnect) and resumes message handling without restart.

### Config

- [ ] **CONFIG-01**: Bot loads a YAML config file at a path supplied via CLI flag or env var (default `./config.yaml`).
- [ ] **CONFIG-02**: Config validates at load time and refuses to start on: invalid group JID, owner JID parse error, unresolvable IANA timezone, matcher distance below min-word-length (distance 1 → ≥5 chars, distance 2 → ≥8 chars), or any answer whose tokens fuzzy-match its own matcher keywords (self-loop guard).
- [ ] **CONFIG-03**: Bot exposes the current config as an immutable snapshot via `atomic.Pointer[Snapshot]`; readers hold a snapshot for the full duration of a message-handling call.
- [ ] **CONFIG-04**: Bot hot-reloads config on YAML file change via `fsnotify` watching the **parent directory** (not the file), with 200–500 ms debounce; on parse/validate failure, bot keeps the previous snapshot and logs WARN.
- [ ] **CONFIG-05**: Bot falls back to a 30–60 s mtime poll if fsnotify reports an unrecoverable error.

### Scope (Hard Gates Before Matching)

- [ ] **SCOPE-01**: Bot listens to exactly one WhatsApp group JID, configured at startup; any message whose `Info.Chat` differs from the configured group is dropped at the adapter's first gate.
- [ ] **SCOPE-02**: Bot drops any message with `Info.IsFromMe == true` immediately after the group-JID gate (self-reply loop prevention). *Promoted from PITFALLS/FEATURES research — was missing from initial PROJECT.md Active list.*
- [ ] **SCOPE-03**: Bot drops non-text message types it does not handle (stickers, audio, view-once) before they reach the matcher.

### Match

- [ ] **MATCH-01**: Bot fuzzy-matches the new message's body text against each matcher's `words` list using Levenshtein distance (`agnivade/levenshtein`).
- [ ] **MATCH-02**: Bot also fuzzy-matches the **quoted-message text** (when the new message quotes a prior one) against the same matcher rules; one match in either input fires.
- [ ] **MATCH-03**: Bot tokenizes each input on whitespace and Unicode word boundaries and matches per-token (not against the whole concatenated body).
- [ ] **MATCH-04**: Bot Unicode-normalizes (NFC) and lowercases both input tokens and matcher keywords before comparing; optional accent-strip is configurable per matcher.
- [ ] **MATCH-05**: Bot enforces the per-distance min-length validation from CONFIG-02 at match time as well (defense in depth).

### Reply

- [ ] **REPLY-01**: When a matcher fires, bot replies with a **WhatsApp threaded reply** quoting the triggering message (`ExtendedTextMessage` + `ContextInfo{StanzaId, Participant, QuotedMessage}`).
- [ ] **REPLY-02**: Bot picks one answer uniformly at random from the triggered matcher's `answers` array.
- [ ] **REPLY-03**: Bot substitutes `{MATCHED_WORD}` (the input token that matched) and `{REPLIED_USER}` (the sender's `PushName`, falling back to JID local part) inside the chosen answer.
- [ ] **REPLY-04**: Bot delays sending by a random 2–8 s jitter before sending (anti-fingerprinting).
- [ ] **REPLY-05**: Bot enforces a global outbound rate cap (default ≤6 replies/min, ≤30/hour) ahead of cooldown checks; replies that would exceed the cap are dropped and logged.

### Cooldown

- [ ] **COOLDOWN-01**: Bot keeps an in-memory per-matcher cooldown (default 5 min, configurable per matcher); a matcher cannot re-fire for any user until its cooldown expires.
- [ ] **COOLDOWN-02**: Bot keeps an in-memory per-user-per-matcher cooldown (default 15 min, configurable globally); the same sender cannot re-trigger the same matcher until expired.
- [ ] **COOLDOWN-03**: Cooldown state uses `time.Since` against monotonic timestamps stored at fire time (no `Unix()` round-trips); state is keyed in a `sync.Map` and reaped by a background ticker.
- [ ] **COOLDOWN-04**: Cooldown state is in-memory only and resets on restart (acceptable for v1; no SQLite tables).

### Quiet Hours

- [ ] **QUIET-01**: Bot honors a `quiet_hours: { start: "22:00", end: "08:00", timezone: "America/Sao_Paulo" }` window in config; any reply attempt during the window is dropped and logged.
- [ ] **QUIET-02**: Bot uses `time.LoadLocation(cfg.Timezone)` (never `time.Local`) and the Docker image bundles `tzdata`.
- [ ] **QUIET-03**: Quiet-hours wrap-around midnight is correctly handled (start > end ⇒ window spans midnight).

### Owner

- [ ] **OWNER-01**: Bot recognizes a hardcoded list of owner WhatsApp JIDs in the YAML config; only those JIDs may issue commands.
- [ ] **OWNER-02**: Owner commands are accepted only via **DM to the bot**, never in the configured group.
- [ ] **OWNER-03**: Bot implements two commands: `!pause` and `!resume`; `!pause` flips an `atomic.Bool` kill switch, `!resume` clears it.
- [ ] **OWNER-04**: When the kill switch is set, the matcher pipeline drops every incoming group message at the first gate — but owner DMs still process (so `!resume` works).
- [ ] **OWNER-05**: Bot acks every owner command with a brief DM reply ("paused" / "resumed") so the operator sees commands landed.
- [ ] **OWNER-06**: Kill switch state lives outside the config snapshot; hot-reload does **not** reset it.

### Deploy

- [ ] **DEPLOY-01**: Bot ships as a multi-stage Docker image: `golang:1.26.3-alpine` builder → `gcr.io/distroless/static-debian13:nonroot` runtime; final image ≤ 25 MB; built with `CGO_ENABLED=0` and `-trimpath -ldflags="-s -w"`.
- [ ] **DEPLOY-02**: A `docker-compose.yml` example mounts the SQLite session DB and YAML config as named volumes / bind mounts, with `restart: unless-stopped` and a 10 s graceful stop period.
- [ ] **DEPLOY-03**: Bot handles `SIGTERM` / `SIGINT` via the existing signal-aware context and shuts down whatsmeow + SQLite + reapers cleanly within 10 s.
- [ ] **DEPLOY-04**: A `.dockerignore` excludes `*.sqlite*` so local session DBs never leak into the image.
- [ ] **DEPLOY-05**: README documents the SQLite backup procedure (stop container → copy `.sqlite` + `-wal` + `-shm` → restart).

### Observ

- [ ] **OBSERV-01**: All bot logs use `log/slog` with structured fields (`matcher`, `user_jid`, `group_jid`, `event`); JSON handler in Docker, text handler in dev.
- [ ] **OBSERV-02**: Bot logs every match decision: matcher fired, dropped by cooldown, dropped by quiet hours, dropped by kill switch, dropped by rate cap — each with reason.
- [ ] **OBSERV-03**: Bot logs whatsmeow lifecycle events (Connected, Disconnected, LoggedOut, PairSuccess) at INFO/ERROR level.

## v2 Requirements (Deferred)

- Extended owner commands: `!status`, `!last`, `!mute <matcher>`, `!quiet <duration>`.
- Persisted cooldowns and kill switch (survive restart) via `state.json` sidecar (no SQLite tables — preserves the "session-only DB" invariant).
- Reaction-as-reply mode (per-matcher `mode: text | reaction | both`, with `🍵` emoji) — strongest camomila brand reinforcement; deferred only because it's not strictly required for v1 utility.
- `dry_run: true` per matcher (log what would have fired without sending).
- `/healthz` HTTP endpoint for container orchestrator probes.
- Per-matcher silent-mode toggle for staged rollout.
- Recent-history dedupe (don't reply twice if same answer was the last thing the bot said).
- SIGHUP fallback for config reload.
- Weighted answer selection (some answers more likely than others).
- Prometheus `/metrics` exporter for ops dashboards.

## Out of Scope

- **Multi-group / multi-tenant** — v1 is single-group only. Adding a second group requires per-group config + per-group cooldown isolation; not a v1 cost.
- **WhatsApp Business / Cloud API** — whatsmeow only. Cloud API needs business verification and is overkill for personal-scale use.
- **LLM-generated replies** — undermines the curated calming tone and adds API cost + hallucination risk; also Meta's January 2026 anti-AI-chatbot policy raises ban risk.
- **Image / video caption matching** — text body and quoted text only.
- **Edited-message re-evaluation** — fire once on original to avoid retroactive spam.
- **Group-admin-based authorization** — owner identity is hardcoded JIDs; avoids coupling kill-switch authority to WhatsApp group-role semantics.
- **Owner WhatsApp commands beyond pause/resume** (add/disable matchers via DM) — deferred to v2.
- **Hundreds of matchers / matcher-management UI** — v1 is ≤10 hand-curated; a UI is a v2 product, not a v1 feature.
- **Full message archival** — only the in-memory state cooldowns require.
- **Anti-detection theatrics** (typing-presence, randomized human delays beyond the 2–8 s jitter) — WhatsApp ban detection inspects protocol fingerprints, not typing UX; jitter + rate cap + low volume is the real defense.
- **Leaderboards, streaks, public scoldings, reaction-farming** — engagement-bait patterns that would directly invert the camomila framing.
- **View-once message handling** — bot *can* read view-once content but quoting it would re-publish ephemeral content; explicit refusal at event boundary.
- **Sentiment analysis / LLM intent classification** — explicit keyword matchers only.

## Traceability

Phase → requirement mapping is filled in by `/gsd:plan-phase` after the roadmap is created.
