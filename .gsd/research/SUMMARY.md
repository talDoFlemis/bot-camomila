# Project Research Summary — bot-camomila

## Executive Summary

`bot-camomila` is a deliberately narrow, single-group WhatsApp reply bot whose purpose is the opposite of every other chat bot in the ecosystem: it should fire **rarely** and **softly** to take the edge off recurring heated topics. The build path the research converges on is unambiguous — a single Go binary using `go.mau.fi/whatsmeow` (the only realistic Go client for WhatsApp Multi-Device), `modernc.org/sqlite` as the **CGO-free** session driver (the choice that keeps the Dockerfile to a 15–20 MB distroless static image), `goccy/go-yaml` + `fsnotify` for hot-reloaded config, `agnivade/levenshtein` for fuzzy matching, and `log/slog` from stdlib. There is no real architectural creativity required; the patterns are well-trodden. The creativity belongs entirely in **what NOT to build** — no LLM, no leaderboards, no escalation/punishment, no anti-detection theatrics. Every one of those would invert the camomila framing.

Architecturally, the bot is a small hexagonal single-binary: one adapter package (`internal/whatsappadapter`) is the *only* place that imports whatsmeow; everything else (`internal/matcher`, `internal/cooldown`, `internal/killswitch`, `internal/config`, `internal/ownercommands`) is pure Go and unit-testable without a WhatsApp connection. The hot path is lock-free: config lives behind `atomic.Pointer[Snapshot]`, cooldowns live in a `sync.Map` with a background reaper, kill switch is `atomic.Bool`. fsnotify watches the **parent directory** (not the file) so editor atomic-rename saves don't silently break reloads.

The risk surface is small but sharp. Three classes of failure dominate: **(a) account-level risk** — WhatsApp can silently log out / ban the paired number for behavioral fingerprinting, so cooldowns, quiet hours, jittered latency, and a hard outbound rate-cap are not optional; **(b) scope-leak risk** — the bot replying outside the configured group (DM, other group, broadcast) is the worst failure mode and must be prevented by a hard JID allow-list check at the adapter boundary plus `IsFromMe` filtering (notably *missing from PROJECT.md Active list*); **(c) reload/state-correctness risk** — fsnotify atomic-rename bug, hot-reload races, Unicode/NFC normalization mismatches, Levenshtein distance:1 false positives on short words, and timezone-less Docker images all silently break the bot in production.

## Key Findings

### Recommended Stack

Single-binary Go service with fully pure-Go dependency tree, runnable as a 15–20 MB `distroless/static-debian13:nonroot` image. CGO-free path (`modernc.org/sqlite`) is what makes this clean.

**Core technologies:**

- **Go 1.26.3** — matches whatsmeow's `toolchain go1.26.3` directive exactly.
- **`go.mau.fi/whatsmeow` pinned to `v0.0.0-20260516102357-8d3700152a69`** — pseudo-version pin is mandatory (no semver tags).
- **`go.mau.fi/whatsmeow/store/sqlstore`** — required device-session persistence; auto-upgrades schema.
- **`modernc.org/sqlite v1.50.1`** — pure-Go driver. **Dialect for sqlstore must be `"sqlite3"` even though driver registers as `"sqlite"`** — use `sqlstore.NewWithDB(db, "sqlite3", waLog)` or register an alias.
- **`goccy/go-yaml v1.19.2`** — actively maintained (yaml.v3 frozen since 2022); `Strict()` catches typo'd keys.
- **`agnivade/levenshtein v1.2.1`** — rune-safe, ~330ns/op.
- **`fsnotify v1.10.1`** — watch the **parent directory**, not the file.
- **`log/slog`** stdlib + ~30-line `waLog.Logger` adapter for whatsmeow.
- **`stretchr/testify v1.11.1`** — skip `suite` (no parallel).
- **`gcr.io/distroless/static-debian13:nonroot`** — bundles `ca-certificates`, `tzdata`, nonroot uid 65532.

### Expected Features

**Must have (v1):** QR pair + persisted session; single-group JID allow-list as hard gate; **`IsFromMe` filter (missing from PROJECT.md Active — promote)**; fuzzy match on body AND quoted text (one engine, two inputs); threaded reply with `ContextInfo`; random pick from `answers` cluster with `{REPLIED_USER}`/`{MATCHED_WORD}` substitution; per-matcher AND per-user cooldowns (stacked); timezone-aware wrap-around quiet hours; owner DM `pause`/`resume` kill switch; hot-reload with atomic swap + pre-swap validation; Docker image + mounted volumes; structured slog; graceful shutdown.

**Should have (v1.x):** Reaction-as-reply mode (`react`/`reply`/`both`, with `🍵`) — strongest camomila brand reinforcement; `dry_run: true` per matcher; `/healthz`; per-matcher silent mode; recent-history dedupe; SIGHUP fallback reload.

**Defer (v2+):** Extended owner commands (`status`/`last`/`mute`/`quiet`), persisted cooldowns, Prometheus, weighted answers, jittered/decaying cooldowns. **Explicit out-of-scope (PROJECT.md):** multi-group, web UI, image/OCR, edited-message re-fire.

**Refused (anti-features):** LLM replies, leaderboards, streaks, scoldings/mutes/kicks, typing-presence theatrics, bot-initiated polls, view-once unwrapping, sentiment analysis. Each inverts the camomila framing.

### Architecture Approach

Hexagonal-in-spirit, single-binary in practice. **The whatsmeow import firewall is the load-bearing boundary:** `internal/whatsappadapter` is the only package that imports `go.mau.fi/whatsmeow`. Hot path is lock-free: `atomic.Pointer[Snapshot]` for config, `sync.Map` + reaper for cooldowns, `atomic.Bool` for kill switch.

**Major components:**

1. **`cmd/bot/main.go`** — ~80-line entrypoint: signal-aware ctx, build adapters, errgroup, await shutdown.
2. **`internal/config`** — YAML load + validate + atomic snapshot store + fsnotify directory watcher.
3. **`internal/domain`** — pure value types; zero whatsmeow imports.
4. **`internal/matcher.Pipeline`** — single `Handle(domain.Message) *MatchResult`; gates kill-switch → quiet-hours → normalize → fuzzy → cooldown → answer-pick.
5. **`internal/cooldown`** — `sync.Map` keyed by `(matcher, userJID)` + injectable `Clock` + background reaper.
6. **`internal/killswitch`** — `atomic.Bool` wrapper.
7. **`internal/whatsappadapter`** — whatsmeow lifecycle (pair/QR/Connect/Disconnect), `*events.Message` ↔ `domain.Message` mapping, threaded `ExtendedTextMessage{ContextInfo{StanzaId, Participant, QuotedMessage}}` send.
8. **`internal/ownercommands`** — routes owner DMs to kill-switch.

**Build order (proven-works-first):** config types → domain types → adapter skeleton with QR pairing → group filter + inbound mapping → matcher pipeline (pure-Go test then wire) → outbound threaded reply → cooldowns → quiet hours → kill switch + owner commands → fsnotify hot-reload → Dockerfile. **Treat match + reply + cooldown as one shippable bundle** — deploying reply without cooldown into a real group is a footgun.

### Critical Pitfalls

1. **Account ban / device-removed from automation fingerprinting** — stacked cooldowns (per-matcher + per-user + per-group floor), random 2–8s reply jitter, quiet hours in fixed TZ, **global rate cap before cooldown check**, single-process lockfile, dedicated phone number, loud handling of `events.LoggedOut` (exit non-zero, never auto-pair).
2. **Scope leak (reply in DM / other group / broadcast / to self)** — allow-list at adapter's first gate; validate configured JID at startup (`Server == types.GroupServer`); drop `IsFromMe == true` immediately after group check; unit-test dispatcher with all message-type variants.
3. **fsnotify hot-reload races + stuck-old-config** — watch parent directory, filter by filename, 200–500ms debounce, parse+validate into fresh snapshot before atomic swap (keep old on error), 30–60s mtime poll as belt-and-suspenders, cooldown state lives **outside** Config.
4. **Levenshtein distance:1 false positives on short words** — tokenize first (don't slide across whole body), enforce min-length per distance at config-load (distance:1 ≥5 chars, distance:2 ≥8), normalize before matching (NFC + lowercase + optional accent strip).
5. **Self-reply loops** — `IsFromMe` as *first* dispatcher gate; startup-time scan of every answer for any matcher keyword within distance; skip quoted-text matching when quoted message author is the bot itself.

Also significant: SQLite session corruption (lockfile + integrity check + graceful shutdown + named volume on local ext4), whatsmeow protocol drift / 405 (pin commit SHA + upgrade discipline), cooldown drift on long uptimes (`time.Since` + in-memory only), wrong timezone (explicit `timezone:` field + `time.LoadLocation` + bundle tzdata), Docker libc mismatch (CGO-free + distroless static), Unicode NFC/NFD mismatch (`norm.NFC.String` on both sides), kill switch reset on reload (owner DM is separate code path; pause state in Runtime, not Config).

## Implications for Roadmap

Research strongly converges on a **6-phase** roadmap. With Coarse granularity setting, the roadmapper may collapse phases 5–6 or 4–5; the convergent minimum is 4 phases (Foundations / Pipeline+Reply+Cooldown / Operability / Deploy).

### Phase 1 — Session & Config Foundations

**Rationale:** Everything needs (a) paired whatsmeow session and (b) validated config snapshot. Pure-Go, fully testable, gates everything later. QR pairing is the riskiest one-time setup — exercise it early.
**Delivers:** Bot pairs via QR, persists session, reloads on restart, parses + validates `config.yaml` (group JID is real group, matchers have sane distance/length, owner JIDs parse, timezone resolves, answers don't echo their own matcher keywords), exposes `Snapshot` via `atomic.Pointer`, graceful shutdown on SIGTERM. No matching/replies yet.
**Avoids:** Pitfalls 1, 3, 5, 6, 8, 9.

### Phase 2 — Matcher Pipeline & Safe Dispatch

**Rationale:** This is the product, but deploy only with cooldowns and quiet hours in place. **First shippable bundle.**
**Delivers:** Full `matcher.Pipeline.Handle` (kill-switch placeholder → quiet-hours → normalize → tokenize → per-token fuzzy with min-length enforcement → per-matcher cooldown → per-user cooldown → random answer pick + variable substitution). Adapter does inbound mapping, hard group-JID gate, `IsFromMe` filter, message-type filter, owner-DM split, threaded outbound reply, 2–8s jittered latency, global outbound rate-cap. Cooldown reaper respects `ctx.Done()`.
**Implements:** `internal/matcher`, `internal/cooldown`, `internal/whatsappadapter`, `internal/killswitch` (no-op gate read).
**Avoids:** Pitfalls 2, 6, 7, 8, 10, 11, partial 1.

### Phase 3 — Owner Commands & Operational Kill Switch

**Rationale:** Bot is live in a group but can't be silenced from a phone. P0 before extended use.
**Delivers:** Owner-DM routing (DM-only + whitelisted JIDs), command dispatcher (`!pause`/`!resume`/`!status`), kill-switch as `atomic.Bool` checked as **first** gate in `Handle`, pause-state in `Runtime` struct **outside** `Config`, ack-reply so operator sees commands landed, owner commands bypass quiet hours.
**Avoids:** Pitfall 12 entirely.

### Phase 4 — Hot-Reload (fsnotify)

**Rationale:** Tune matchers without re-pairing. Deferred until after kill switch because atomic-rename quirks demand integration testing against real editors/Docker volumes; bot is fully usable with restart-on-config-change.
**Delivers:** fsnotify watcher on **parent directory**, filename-filtered, 200–500ms debounce, parse + validate into fresh `*Snapshot`, atomic swap on success, keep-old + WARN on error, 30–60s mtime poll fallback, INFO log on every reload outcome.
**Avoids:** Pitfall 4 entirely. Reinforces Pitfall 12.

### Phase 5 — Docker Packaging & Deployment

**Rationale:** All functionality in place; package for VPS. Deferred to end because Dockerfile depends on final dependency set and smoke-testing requires a working bot.
**Delivers:** Multi-stage Dockerfile (`golang:1.26.3-alpine` builder → `gcr.io/distroless/static-debian13:nonroot` runtime), `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`, 15–20 MB image, named-volume guidance for SQLite (refuse NFS), `docker compose` example with `restart: unless-stopped` + 10s graceful stop, CI smoke-test, `.dockerignore` excluding `*.sqlite*`, README backup procedure (stop → copy `.sqlite` + `-wal` + `-shm` → start).
**Avoids:** Pitfalls 3, 8, 9; reinforces 1.

### Phase 6 — v1.x Differentiators (Post-Validation, optional)

**Rationale:** Only after v1 has run in the real group for an extended cycle; each item triggers off an observed signal.
**Delivers (each independently shippable):** Reaction-as-reply mode (per-matcher `mode: text | reaction | both`), `dry_run: true` per matcher, `/healthz`, per-matcher silent mode, recent-history dedupe, SIGHUP fallback reload.

### Phase Ordering Rationale

- **Dependencies:** Pairing (1) gates everything. Inbound mapping (2) gates outbound testing. Match + cooldown + reply ship together (footgun otherwise). Kill switch (3) before extended live use. Hot-reload (4) is independent. Docker (5) wraps everything.
- **Pitfall avoidance by design, not retrofit:** Critical pitfalls 1, 2, 4, 6, 10, 11 are addressed in Phases 1–4. The PROJECT.md gap (`IsFromMe` filter) becomes a Phase 2 hard requirement. Stacked cooldowns + jitter + rate cap all land in Phase 2 — partial implementation is worse than none.

### Research Flags

**Needs deeper research during planning:**

- **Phase 1:** whatsmeow QR terminal-render UX (`mdp/qrterminal/v3`?) + QR-expiry handling; sqlstore dialect-alias pattern (`NewWithDB` vs registered alias — pick one); first-pair `HistorySync` flood (filter events by `evt.Info.Timestamp` predating bot start).
- **Phase 2:** Per-token Levenshtein vs whole-message trade-offs (Portuguese tokenization rules); `waE2E.Message` exact ContextInfo construction for threaded replies; multi-device JID-normalization discipline (`xxxxxxxxxx:NN@s.whatsapp.net` vs mobile — `.ToNonAD()` everywhere).
- **Phase 4:** fsnotify debounce window tuning against the operator's actual editor + Docker volume backend (MEDIUM-confidence number in PITFALLS.md).
- **Phase 5:** Named-volume vs bind-mount choice on target VPS filesystem.

**Standard patterns (skip research-phase):**

- **Phase 3 (Owner Commands & Kill Switch):** Fully spelled out in ARCHITECTURE.md.
- **Phase 6 (v1.x Differentiators):** Each item is a small, well-scoped addition on top of v1 surface.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All versions verified against pkg.go.dev / upstream go.mod / WebFetch. Caveat: pseudo-version pin needs re-verification per upgrade. |
| Features | HIGH (whatsmeow message-shape) / MEDIUM (cross-platform patterns) | No published WhatsApp open-source de-escalation bot exists; camomila is a deliberate inversion. |
| Architecture | HIGH (Go stdlib + whatsmeow + fsnotify patterns) / MEDIUM (debounce window) | Textbook patterns verified against official docs. Only empirical tuning point: fsnotify debounce. |
| Pitfalls | HIGH (whatsmeow / fsnotify / SQLite / Docker) / MEDIUM (ban-rate heuristics) | Pitfalls 2–12 verified against upstream issues. Pitfall 1 thresholds unpublished; prevention strategies HIGH-confidence. |

**Overall confidence:** HIGH

### Gaps to Address

- **Exact whatsmeow ban thresholds** — unpublished. Mitigation: low volume by design, dedicated number, `events.LoggedOut` as P0. Validate empirically over 2-week post-v1 observation.
- **fsnotify debounce window for operator's editor/Docker setup** — 200–500ms is a starting range; tune in Phase 4.
- **First-pair `HistorySync` behavior** — disable history sync via whatsmeow option, OR timestamp-filter events to drop pre-start.
- **`{REPLIED_USER}` resolution** — `Info.PushName`? Phone fallback? Strip `@s.whatsapp.net`? Decide in Phase 2 requirements.
- **Cooldown precedence (per-matcher vs per-user)** — bias is per-matcher first (cheaper), but log-explanation semantics need documentation in Phase 2.
- **Owner-command syntax** — `!pause` vs `/pause` vs plain `pause`. Decide once in Phase 3.
- **Pause-survives-restart** — bias is "no, in-memory" for v1 simplicity. Validate in Phase 3; if persisted, use `state.json` (not a SQLite table) to keep "no app DB tables" invariant.

## PROJECT.md Gap Flagged

The `IsFromMe` filter (preventing self-reply loops) is documented as critical in PITFALLS.md and FEATURES.md but **missing from PROJECT.md's Active requirements list**. The requirements-definition step should promote it to a v1 hard requirement (suggested REQ-ID `MATCH-04` or similar in the matcher/dispatch category).

## Sources

**Primary (HIGH):** pkg.go.dev for whatsmeow / sqlstore / modernc.org/sqlite / goccy/go-yaml / agnivade/levenshtein / fsnotify / testify; tulir/whatsmeow source; whatsmeow GitHub issues #810, #561, #807 and discussions #199, #388, #567, #568; whatsapp-mcp issues #216, #153; GoogleContainerTools/distroless; sync/atomic + Go time monotonic-clock docs; Simon Willison SQLite-WAL-in-Docker 2026.

**Secondary (MEDIUM):** Cross-platform competitor analysis; Meta WhatsApp AI ban (Jan 2026); anti-ban posture guides; thoughtbot per-user rate limits; discord.py cooldown patterns; Match Data Studio Levenshtein vs Jaro-Winkler; hexagonal Go architecture; Standard Go Project Layout; fsnotify debounce patterns.

**Tertiary (LOW — needs validation):** Anecdotal whatsmeow ban thresholds; fsnotify debounce window (200–500ms).
