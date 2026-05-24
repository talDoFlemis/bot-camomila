# bot-camomila

## What This Is

WhatsApp de-escalation bot for one group chat. Watches messages, fuzzy-matches keywords (Levenshtein distance), and replies with a randomly-picked calming response — threaded to the triggering message. Named for chamomile tea: the bot's job is to take the edge off recurring heated topics (e.g., SEFAZ rants).

## Core Value

When a configured topic flares up in the target group, the bot reliably replies once with a soothing canned answer — without spamming, without leaking outside the group, and without going down.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- [x] Owner kill switch: `!pause` / `!resume` from configured group pauses/resumes bot without restart (Validated in Phase 3: owner-commands-operability)
- [x] Hardcoded owner JID list in YAML; LID addressing handled via GetGroupInfo resolution (Validated in Phase 3)

### Active

<!-- Current scope. Building toward these. -->

- [ ] Pair to WhatsApp via whatsmeow QR flow and persist session in SQLite
- [ ] Listen only to one configured group JID; ignore DMs and other groups
- [ ] Match keywords with per-matcher Levenshtein distance against new message text AND quoted message text
- [ ] Reply with a random entry from the matcher's `answers` array, as a WhatsApp threaded reply to the triggering message
- [ ] Per-matcher cooldown to avoid spam-on-spam
- [ ] Per-user cooldown to ignore the same sender retriggering rapidly
- [ ] Quiet hours window during which the bot stays silent
- [ ] Owner kill switch: an owner DM command pauses/resumes the bot without restart
- [ ] Hot-reload matchers/config on YAML file change (fsnotify)
- [ ] Hardcoded owner JID list in YAML (for kill switch + commands)
- [ ] Ship as a Docker image with mounted volumes for SQLite session + YAML config

### Out of Scope

<!-- Explicit boundaries. Includes reasoning to prevent re-adding. -->

- Multi-group / multi-tenant support — v1 is single-group only
- Image/video caption matching — text body and quoted text only
- Re-evaluating edited messages — fire once on original
- LLM-generated replies — answers are static, curated, random-pick only
- Owner WhatsApp commands beyond the kill switch (add/disable matcher via DM) — defer to v2
- Group-admin-based authorization — owner identity is hardcoded JIDs only
- Hundreds of matchers / matcher-management UI — v1 is ≤10 hand-curated
- Full message archival — only what cooldown state requires
- WhatsApp Cloud / Business API integration — whatsmeow only
- Anti-detection theatrics (typing presence, randomized human delays) — not in v1

## Context

- Existing scaffold: `main.go` (signal-aware context loop with `log/slog`), `config.go` (empty `Config`, `DBConfig`, `MatcherConfig`, `AnswerConfig` structs), `config.example.yaml` showing one matcher (`name: sefaz`, `words: [SEFAZ]`, `distance: 1`).
- Go 1.26.3 module `github.com/taldoflemis/bot-camomila`. Standard library `log/slog` already in use.
- whatsmeow is the canonical Go WhatsApp client; it requires SQLite for its device session store — this is the only justification for a DB in v1.
- "camomila" framing implies de-escalation, not engagement: bot should fire rarely and softly. Cooldowns + quiet hours are first-class, not afterthoughts.

## Constraints

- **Tech stack**: Go 1.26.3; whatsmeow for WhatsApp; SQLite (modernc.org/sqlite or mattn/go-sqlite3) for whatsmeow session store; fsnotify for config hot-reload; `log/slog` for logging.
- **Scope**: Single WhatsApp group, hardcoded by JID in config.
- **Deployment**: Docker container on a VPS; SQLite + YAML mounted as volumes; QR pairing happens once at first launch and survives restarts via persisted session.
- **Persistence**: Only whatsmeow session in SQLite. No app-level DB tables in v1 (cooldown state in-memory; restart resets cooldowns — acceptable).
- **Safety**: Bot must never reply outside the configured group; must respect quiet hours; must obey kill switch immediately on receipt.

## Key Decisions

| Decision | Rationale | Outcome |
|---|---|---|
| whatsmeow (not Baileys / Cloud API) | Native Go, no subprocess, no business verification, sufficient for single-group personal use | — Pending |
| Single-group-only by JID | Smaller surface; eliminates cross-tenant leakage and authorization complexity for v1 | — Pending |
| Threaded reply quoting original | Makes intent obvious; avoids polluting unrelated threads | — Pending |
| Random pick from answers array (no LLM) | Predictable, curated, no API cost, no hallucinations | — Pending |
| Hot-reload via fsnotify | Tune matchers without restarting and re-pairing | — Pending |
| In-memory cooldowns (no DB rows) | v1 simplicity; restart-reset cooldowns acceptable | — Pending |
| Hardcoded owner JIDs (not group admins) | Avoid coupling kill-switch authority to WhatsApp group-role semantics | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-05-24 after Phase 3 (owner-commands-operability) complete*
