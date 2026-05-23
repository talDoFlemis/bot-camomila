# Feature Research

**Domain:** WhatsApp single-group de-escalation / canned-reply bot (whatsmeow, Go)
**Researched:** 2026-05-22
**Confidence:** HIGH for whatsmeow message-shape features (verified against tulir/whatsmeow source + discussions); MEDIUM for cross-platform pattern claims (WhatsApp auto-reply / Discord auto-mod ecosystem); LOW where noted (no formalized "calm bot" pattern exists in the literature yet — design judgment applied).

## Feature Landscape

Categories below are scoped to a **single-group, opinionated, low-volume de-escalation bot**. A feature being "table stakes" here means a user dropping the bot into their group would feel its absence as a defect; "differentiator" means it would meaningfully strengthen the camomila framing past v1; "anti-feature" means it actively contradicts the calming framing and should be refused even if requested.

### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| QR-pair to user's WhatsApp once, persist session | whatsmeow has no headless server-side login — the only path. Re-pairing on every restart would be a dealbreaker. | M | whatsmeow `sqlstore` device store in SQLite; survive container restarts via mounted volume. Already decided. |
| Single-group JID allowlist (ignore DMs + other groups) | Bot reaching outside the configured group is the worst-case failure for a "soothing" tool. Bot identity is shared across the user's whole WhatsApp account. | S | Drop any inbound event where `Info.Chat` != configured group JID, BEFORE any matching. Hard guard, not a filter. Already decided. |
| Fuzzy keyword match on message body | The whole point. "SEFAZ" vs "sefaz!" vs "sefazz" all need to fire. | S | `agnivade/levenshtein` or `hbollon/go-edlib`. Tokenize first (normalize whitespace, lowercase, strip punctuation), then per-token Levenshtein against each `words` entry with the matcher's `distance`. |
| Fuzzy match against quoted message text | A user quote-replying to an older "SEFAZ" rant is the *same* trigger event from a UX perspective. | S | whatsmeow exposes `ContextInfo.QuotedMessage`; extract its conversation text and run the same matcher. Already decided. |
| Threaded reply quoting the triggering message | Without quoting, the bot's reply looks like a non-sequitur in a fast-moving group and re-introduces noise instead of reducing it. | S | Use `ExtendedTextMessage` with `ContextInfo{StanzaId, Participant, QuotedMessage}` — well-trodden whatsmeow pattern. Already decided. |
| Random pick from a curated answers array | Repeating the *exact* same line every time turns the bot into a meme/spam target. Variety = perceived softness. | S | `math/rand/v2` over `cluster.answers`. No weighting needed v1. Already decided. |
| Per-matcher cooldown | Without it, one heated thread = 20 bot replies = bot becomes the noise. | S | In-memory `map[matcherName]time.Time`; cooldown duration is per-matcher config. Already decided. |
| Per-user cooldown | One angry sender re-typing the trigger should not retrigger the bot. The bot answers a *topic*, not a *user*. | S | In-memory `map[matcherName+senderJID]time.Time`. Already decided. |
| Quiet hours (timezone-aware window of silence) | A bot replying at 3 a.m. is the opposite of calming. Especially true if the trigger word is something legitimately discussed late at night. | S | Single window per config (`start`, `end`, `timezone`); check against `time.Now().In(tz)`. Handle wrap-around (22:00–07:00). Already decided. |
| Owner kill-switch via DM command | If the bot misbehaves in front of the group, the owner must be able to silence it from their phone immediately, without SSH. | S | Hardcoded owner JIDs in YAML; accept commands only from DMs (`Info.Chat == sender JID`) from those JIDs. Commands: `pause`, `resume`, `status`. Already decided. |
| Hot-reload YAML on file change | Tuning matchers (adding words, adjusting distance, adding answers) without re-pairing the WhatsApp session is essential — re-pair = QR scan = friction. | M | `fsnotify` watching the config path; reload + swap atomically via mutex/`atomic.Pointer`. Validate before swap; on parse error keep old config and log. Already decided. |
| Ignore self-sent messages | whatsmeow delivers your own outbound messages back as events. Without a self-filter the bot will trigger on its own replies and loop. | S | Skip events where `Info.IsFromMe == true`. **Not yet explicit in PROJECT.md — flag for REQUIREMENTS.** |
| Ignore the bot's own canned answers as triggers | Even with self-filter, a copy/paste of the bot's reply by a human shouldn't escalate. Avoid Ouroboros via re-trigger via cooldown — already covered, but documented. | S | Covered by per-matcher cooldown if tuned right; consider also a hash-of-recent-outbound short-term skiplist. |
| Graceful shutdown on SIGINT/SIGTERM | Required for clean Docker stop. whatsmeow needs `client.Disconnect()` so the WS closes properly and the device record isn't left in a weird state. | S | Already scaffolded via `main.go` signal-aware context. |
| Structured logs (slog) of trigger → match → decision → outcome | Operator needs to answer "why did/didn't the bot reply?" without enabling DEBUG. | S | Already in stack. Log: incoming message ID, matcher hit, cooldown decision, send result. Redact body if privacy needed. |
| Survive transient whatsmeow disconnects | The library reconnects automatically, but the bot process must not exit on a single disconnect. | S | whatsmeow handles WS reconnect internally; just don't `os.Exit` on disconnect events. Log + observe. |

### Differentiators (Competitive Advantage / v2+)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Reply with an emoji **reaction** instead of (or in addition to) a text reply | Reactions are the lowest-noise way to "acknowledge" a trigger. "🍵" on the offending message is even *more* on-brand than a text reply, and adds zero lines to the group history. | S | whatsmeow `BuildReaction(chat, sender, msgID, "🍵")`. Per-matcher config: `mode: text | reaction | both`. Strongest differentiator for the camomila framing. |
| Template variables in answers (`{REPLIED_USER}`, `{MATCHED_WORD}`, `{TIME_OF_DAY}`) | Already implied by `config.example.yaml` (`{REPLIED_USER}`, `{MATCHED_WORD}`). Promotes them to a real feature with documented variables. | S | Simple string replace at send time. Resolve `{REPLIED_USER}` to push-name from `Info.PushName` (fall back to phone). |
| Per-matcher quiet hours override | Some triggers (e.g., a work-hours-only joke) make sense to silence outside business hours even when global quiet hours aren't active. | S | Optional per-matcher window; if absent, fall back to global. |
| Per-matcher mode toggle: `react` / `reply` / `silent` | Lets the operator "soft-disable" a matcher (silent = log only) for tuning without removing it from YAML. | S | Adds a `mode` field. Pairs with hot-reload. |
| Cooldown that decays / jitters | Fixed cooldowns are predictable — users learn "I can re-trigger after 5 min." A jittered or exponentially-extending cooldown (longer if retriggered within window) feels more like a human ignoring you. | M | Track `last_fire + n_recent`; cooldown = base × 2^n_recent clamped. |
| Optional answer weights | If a particular calming phrase is the operator's favorite, allow `weight: 3`. Currently uniform random. | S | `[]struct{Text string; Weight int}`; weighted pick. |
| Local Prometheus `/metrics` endpoint | Triggers fired, cooldowns hit, send errors, reconnects — basic observability so the operator can answer "is it alive and working?" without grepping logs. | M | `promhttp` on a local port; bind to localhost. Optional but a clear v1.x add. |
| Health endpoint / liveness file | For Docker `HEALTHCHECK` — distinguish "process up" from "WS connected and logged in." | S | Simple `/healthz` reporting `client.IsConnected()` and `client.IsLoggedIn()`. |
| `dry_run: true` config flag | New matcher? Run it for 24h logging "would have fired" without actually sending. Crucial to avoid embarrassing false positives in a live group. | S | Per-matcher boolean; on hit, log instead of send. |
| Recent-history dedupe (don't react to identical body within N seconds) | Two near-simultaneous messages with the same text shouldn't both trigger. Per-user cooldown covers some, but not different users posting the same thing. | S | Short-TTL LRU on normalized body hash. |
| Owner-DM commands beyond pause/resume (`status`, `last`, `mute <matcher>`, `quiet`) | Already deferred to v2 in PROJECT.md, but worth keeping the door open. Operator power-user surface. | M | Reuses the kill-switch dispatcher. |
| Persisted cooldown state across restarts | In-memory cooldowns reset on restart — currently accepted ("restart resets cooldowns"). For a bot that auto-deploys, this could cause a burst right after deploy. | M | Optional: persist cooldown map to a tiny BoltDB/JSON file. Only worth it if restart-burst is observed in practice. |
| Per-matcher allowlist of trigger contexts (e.g., only fire on quoted messages, only on new messages) | Some matchers might only make sense as quote-replies; others only on fresh messages. | S | `trigger_on: [new, quoted]` enum list. |
| Reload signal via `SIGHUP` as alternative to fsnotify | Belt-and-suspenders: some bind mounts on certain filesystems don't fire fsnotify events reliably. | S | Cheap to add alongside fsnotify. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **LLM-generated replies** (GPT/Claude in place of canned answers) | "Smarter" responses; less repetitive; can reference message content. | Hallucination directly undermines the curated calming tone — an off-tone or sarcastic LLM reply *escalates* the very thing camomila is supposed to soften. Adds API cost, latency, key management, network dependency. Adds a new attack surface (prompt injection from group chat). Meta is also actively de-platforming general-purpose AI on WhatsApp as of Jan 2026 — wrong-side-of-the-policy risk. | Hand-curated answers array with rotation + variables. Already the decision. |
| **Leaderboards / "most triggered user" / scoreboards** | Engagement gimmicks; "fun" group dynamic. | Creates a competition to *trigger the bot*. Turns the bot from a calming presence into a game piece. Direct inversion of camomila framing. | No accumulation, no public counters. If counters exist at all, they live in private metrics for the operator. |
| **Streaks / daily check-ins / activity rewards** | Borrowed from "wholesome" Discord bots (Aki Streaks etc.). | Same as leaderboards: incentivizes presence and engagement, which is engagement-bait psychology. The bot's goal is the opposite — fewer fires, not more. | Bot stays invisible until needed. Success = nobody notices it. |
| **Reaction-farming / forced reaction prompts** ("React 🍵 to agree") | Builds a sense of community ritual. | Trains the group to perform for the bot. Adds notification spam (every reaction pings the bot's account). | Bot can *send* a reaction; never *asks for* one. |
| **Public scoldings / warning escalation / mutes / kicks** (Discord-style automod) | The "AutoMod" mental model from Discord/Telegram. | (a) whatsmeow can remove participants, but using a personal account to do so is a great way to get socially kicked out of the group. (b) Punitive escalation directly contradicts "de-escalation." (c) Group-admin authority is hard to model cleanly when owner identity is by JID not by role. | Bot replies softly and that's it. Humans handle moderation. |
| **Typing presence / "human-like" delays / read receipts** | "Looks more natural." Cargo-culted from anti-ban guides. | Already in PROJECT.md Out of Scope as "anti-detection theatrics." Adds complexity, slows responses, leaks online status. The bot is *transparently* a bot — pretending otherwise is dishonest and doesn't actually help: WhatsApp's ban detection is more sophisticated than "did it send a typing indicator." | Identify as a bot in the answers themselves. Volume is naturally low (cooldowns + quiet hours) — that's the real anti-ban posture. |
| **Polls / quizzes initiated by the bot** | whatsmeow exposes `BuildPollCreation`. Tempting because it's a flashy capability. | Bot-initiated polls in a group are an interruption. Polls fit "decide together" tools, not "stay calm" tools. Adds protocol complexity (poll votes are encrypted differently and need separate handling). | If polls are ever wanted, build a dedicated tool. Keep camomila monomaniacal. |
| **Broadcast lists / forwarding to multiple groups / cross-group propagation** | "Why not run it in all my groups?" | Explicitly out of scope in PROJECT.md (single-group only). Multiplies risk: a bad matcher fires in 12 groups instead of 1. Tenant isolation is a real architecture cost not paid in v1. | Run a second instance, second JID-bound config, if needed. Each group = a deliberate decision. |
| **Image / video / sticker / caption matching, OCR** | "But people post screenshots of SEFAZ!" | Out of scope in PROJECT.md (text + quoted text only). OCR is expensive, error-prone, and increases the false-positive surface enormously. Also raises privacy concerns (the bot is now "reading" images shared in a private group). | Document as a known limitation. If users post images, they post images — bot stays silent. |
| **Re-fire on edited messages** | Seems "correct" — match should re-evaluate. | Edits are easily abused to retrigger the bot (edit "SEFAZ" → "sefaz" → "SEFAZZ"). Each edit = another reply = noise. Out of scope in PROJECT.md (fire once on original). | Match on original send only. Ignore `MessageEdit` events. |
| **View-once message handling / unwrapping** | whatsmeow can read view-once content. Curiosity feature. | (a) Reading view-once messages and quoting them in a reply is a *trust violation* — the sender chose ephemerality. (b) Quoting a view-once message in a bot reply re-publishes its content. Direct social damage. | Drop view-once messages at the event boundary. Don't match, don't reply. Document the choice. |
| **Group-admin role-based authorization for commands** | "Let group admins also kill the bot." | Out of scope in PROJECT.md (hardcoded JIDs only). Couples the kill-switch to WhatsApp's role semantics, which can be mutated by anyone with admin (including socially-engineered demotions). | Hardcoded owner JIDs. Owner can edit YAML to change them. |
| **Per-user opt-out commands** ("/silence me") | Politeness gesture. | Adds command-parsing surface in the group, adds per-user state, and the bot already reacts so rarely (cooldowns + quiet hours) that the opt-out value is marginal. | Per-user cooldown + the option for the operator to disable a matcher entirely via YAML. |
| **Web admin UI / dashboard for matchers** | "Easier than YAML." | Adds web server, auth, persistence, attack surface — for a single-operator personal-use bot. Out of scope per PROJECT.md ("Hundreds of matchers / matcher-management UI — v1 is ≤10 hand-curated"). | YAML + hot-reload. Operator edits in their editor of choice. |
| **Sentiment analysis / "detect anger" auto-trigger** | "Smarter than keywords!" | Black-box; false positives = bot replies to non-heated messages, which is the *exact* failure mode camomila exists to prevent. Adds an ML dependency. Privacy concern (scanning all messages through a model). | Curated keyword list. Operator knows their group's flashpoints. |
| **Multi-language / translation of triggers** | "What if someone says it in English vs Portuguese?" | If wanted, add both spellings to `words`. Auto-translation introduces an external API and false positives. | List variants explicitly in `words`. Levenshtein + multiple `words` entries already covers most natural variation. |

## Feature Dependencies

```
QR-pair + persisted session
    └──requires──> SQLite store (whatsmeow sqlstore)

Group JID allowlist
    └──requires──> Configured group JID (config schema)

Fuzzy keyword match (body)
    └──requires──> Tokenization + normalization layer
                       └──requires──> matcher.distance threshold in config

Fuzzy match on quoted text
    └──requires──> ContextInfo.QuotedMessage extraction
    └──reuses────> Fuzzy keyword match (body)

Threaded reply
    └──requires──> Original message Info (ID, Sender) preserved through dispatch

Random-pick answer
    └──requires──> answers_cluster lookup by matcher.cluster name
    └──requires──> {REPLIED_USER}/{MATCHED_WORD} variable substitution

Per-matcher cooldown
    └──requires──> In-memory cooldown map keyed by matcher name
    └──conflicts──> Stateless reload of cooldowns (intentional v1 tradeoff)

Per-user cooldown
    └──requires──> Sender JID extraction from Info.Sender
    └──requires──> In-memory cooldown map keyed by (matcher, user)

Quiet hours
    └──requires──> Timezone in config
    └──requires──> Wrap-around-aware time-window check

Owner kill switch (pause/resume)
    └──requires──> Owner JID allowlist in config
    └──requires──> DM detection (Info.Chat == Sender)
    └──requires──> Atomic pause flag (checked before send)

Hot-reload (fsnotify)
    └──requires──> Config validation pre-swap
    └──requires──> Atomic config swap (atomic.Pointer / RWMutex)
    └──enhances──> Per-matcher mode toggle (silent/react/reply)
    └──enhances──> dry_run flag

Reactions as reply mode (differentiator)
    └──requires──> Per-matcher mode field
    └──reuses────> Same trigger pipeline as text reply

Prometheus metrics (differentiator)
    └──requires──> Local HTTP server
    └──conflicts──> "no extra ports" if deployed behind strict network policy
                    (mitigation: bind localhost; opt-in)

Persisted cooldowns (differentiator)
    └──requires──> Small persistence layer (BoltDB or JSON file)
    └──conflicts──> "no app-level DB tables in v1" — defer

Ignore-self-message filter
    └──requires──> Info.IsFromMe check
    └──blocks────> Reply loops (without this, ANY answer that contains a trigger word loops)
```

### Dependency Notes

- **Fuzzy match (body) and fuzzy match (quoted) share the same matcher engine:** Treat them as one feature with two input sources. Don't build them as separate code paths.
- **Threaded reply requires the original `Info` struct to survive through cooldown/quiet-hours checks:** Make the trigger pipeline pass a `TriggerContext{Info, MatchedWord, Matcher}` value, not just a matcher name. Avoids re-fetching.
- **Hot-reload + atomic config swap is a hard dependency for every config-driven feature:** Race-prone. Build the swap mechanism *once* (in Phase 1 or as soon as config types exist) and have all readers use the same accessor.
- **Ignore-self-message filter is a silent prerequisite for every reply mode (text or reaction):** Promote to a Phase 0 requirement. Without it, the first reply that contains the trigger word will infinitely loop. **This is a gap in current PROJECT.md Active list.**
- **Per-matcher cooldown and per-user cooldown stack (both must pass):** Document the precedence — both gates must allow the fire. Cheaper one (per-matcher, single map lookup) checked first.
- **Reactions-as-reply (differentiator) shares everything except the send call:** Cleanly slottable into v1.x once text reply works. Recommend designing the send step behind an interface from day one to make this trivial later.
- **Persisted cooldowns conflict with the v1 decision "no app-level DB tables":** Honor the v1 decision. Revisit only if restart-burst behavior is observed.
- **Owner DM commands extend cleanly: pause/resume in v1 → status/mute in v1.x:** Build the command dispatcher to take a `command → handler` map even if v1 only registers two handlers. Cheap forward-compat.

## MVP Definition

### Launch With (v1)

Minimum to validate "does this actually reduce the heat around recurring topics in our one group?"

- [ ] **QR-pair + session persistence** — without this the bot doesn't connect.
- [ ] **Single-group JID allowlist (hard gate at event boundary)** — required for safety, not optional.
- [ ] **Ignore self-sent messages (`Info.IsFromMe`)** — prevents reply loops. *Missing from PROJECT.md Active; promote.*
- [ ] **Fuzzy match on message body** — core mechanic.
- [ ] **Fuzzy match on quoted text** — completes the "topic flare" model.
- [ ] **Threaded reply with quote** — required for the reply to make sense in flowing group chat.
- [ ] **Random pick from answers cluster with `{REPLIED_USER}` / `{MATCHED_WORD}` variables** — already implied by config.example.yaml.
- [ ] **Per-matcher cooldown** — without it, the bot becomes the noise.
- [ ] **Per-user cooldown** — without it, one user can grief the bot.
- [ ] **Quiet hours (timezone-aware, wrap-around-aware)** — required by camomila framing.
- [ ] **Owner DM kill switch (pause / resume)** — required as the operator's "oh no" button.
- [ ] **YAML hot-reload via fsnotify with atomic swap + validation** — required for matcher iteration without re-pairing.
- [ ] **Docker image + mounted volumes for SQLite session + YAML** — deployment target per PROJECT.md.
- [ ] **Structured slog logging of trigger → decision → send result** — required to debug "why didn't it fire?"
- [ ] **Graceful shutdown on SIGINT/SIGTERM** — required for clean container restarts.

### Add After Validation (v1.x)

Triggered by *observed* signals after v1 runs in the real group.

- [ ] **Reaction-as-reply mode (per-matcher)** — add as soon as v1 is stable. Strongest reinforcement of the camomila framing.
- [ ] **`dry_run: true` per-matcher** — add the moment you want to try a new aggressive matcher without risking a public misfire.
- [ ] **Health endpoint (`/healthz`)** — add when wired into Docker `HEALTHCHECK` or any uptime monitor.
- [ ] **Prometheus `/metrics`** — add the first time you ask "how often is it firing?" and find yourself grepping logs.
- [ ] **Recent-history dedupe** — add the first time two users post the same trigger seconds apart and the bot replies twice.
- [ ] **Per-matcher silent mode** — add when you want to disable a matcher without deleting it. Often arrives with `dry_run`.
- [ ] **SIGHUP-triggered reload** — add the first time fsnotify misses an event on a bind mount.
- [ ] **Jittered / decaying cooldowns** — add only if predictable cooldowns are observed to be exploited.

### Future Consideration (v2+)

Defer until v1 has been validated for at least one extended-use cycle in the target group.

- [ ] **Extended owner DM commands** (`status`, `last`, `mute <matcher>`, `quiet`) — PROJECT.md already defers this. Worth it when v1 proves the kill-switch dispatcher pattern works.
- [ ] **Persisted cooldown state** — defer until restart-burst is observed.
- [ ] **Per-matcher allowlist of trigger contexts** (`new` vs `quoted` only) — defer until a real matcher *needs* this.
- [ ] **Answer weights** — defer; uniform random is rarely the problem.
- [ ] **Multi-group / multi-tenant** — defer indefinitely; PROJECT.md scope says no.
- [ ] **Web UI / dashboard** — defer indefinitely; out of scope.
- [ ] **Image/OCR matching** — defer indefinitely; out of scope.

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| QR-pair + persisted session | HIGH | MEDIUM | P1 |
| Group JID allowlist (hard gate) | HIGH (safety-critical) | LOW | P1 |
| Ignore self-sent messages | HIGH (correctness-critical) | LOW | P1 |
| Fuzzy match on body | HIGH | LOW | P1 |
| Fuzzy match on quoted text | HIGH | LOW | P1 |
| Threaded reply with quote | HIGH | LOW | P1 |
| Random pick + answer variables | HIGH | LOW | P1 |
| Per-matcher cooldown | HIGH | LOW | P1 |
| Per-user cooldown | HIGH | LOW | P1 |
| Quiet hours | HIGH | LOW | P1 |
| Owner DM kill switch | HIGH | LOW | P1 |
| Hot-reload YAML (atomic) | HIGH | MEDIUM | P1 |
| Docker image + volumes | HIGH | LOW | P1 |
| Structured slog logging | MEDIUM | LOW | P1 |
| Graceful shutdown | MEDIUM | LOW | P1 |
| Reaction-as-reply mode | HIGH (brand fit) | LOW | P2 |
| `dry_run` per matcher | MEDIUM | LOW | P2 |
| Health endpoint | MEDIUM | LOW | P2 |
| Prometheus /metrics | MEDIUM | MEDIUM | P2 |
| Recent-history dedupe | MEDIUM | LOW | P2 |
| Per-matcher silent mode | MEDIUM | LOW | P2 |
| SIGHUP reload | LOW | LOW | P2 |
| Jittered cooldowns | LOW | MEDIUM | P3 |
| Extended owner DM commands | MEDIUM | MEDIUM | P3 |
| Persisted cooldowns | LOW | MEDIUM | P3 |
| Trigger-context allowlist | LOW | LOW | P3 |
| Answer weights | LOW | LOW | P3 |

**Priority key:**
- P1: Must have for v1 launch
- P2: Add after v1 validation (v1.x)
- P3: v2+ / defer until observed need

## Competitor Feature Analysis

The "competitor" set here is deliberately cross-platform — there is no widely-known WhatsApp open-source de-escalation bot. The closest analogs are auto-reply WhatsApp tools (engagement-oriented), Discord auto-mod bots (punitive-oriented), and Telegram keyword-reply bots (engagement-oriented). Camomila is a deliberate inversion of all three.

| Feature | WhatsApp auto-reply tools (SleekFlow, Verloop, ManyChat, Chatimize) | Discord auto-mod bots (MEE6, VibeBot, AutoMod, Amanda AI) | Telegram keyword bots (custom + grammY-style) | Camomila approach |
|---------|---------------------------------------------------------------------|------------------------------------------------------------|-----------------------------------------------|-------------------|
| Trigger source | Keyword + intent (often LLM-enriched) | Keyword + regex + spam heuristics + AI toxicity | Keyword + simple regex | Fuzzy keyword (Levenshtein), curated, small set |
| Reply generation | Templated or LLM-generated | Templated warnings + escalating punishments | Templated | **Hand-curated random pick, never LLM** |
| Scope | 1:1 DMs primarily (group auto-reply is rarely supported; WhatsApp Business excludes groups) | Whole server, multi-channel | Whole chat or per-chat | **Single group, JID-bound** |
| Cooldowns | Often missing or session-level only | Per-command (discord.py cooldown decorator) | Often missing — common pitfall | **Per-matcher AND per-user, both enforced** |
| Quiet hours | Office-hours scheduling common | Rarely present (24/7 moderation expectation) | Rarely present | **First-class, timezone-aware** |
| Escalation model | Engagement-oriented (more replies = good) | Warn → mute → kick → ban | Engagement-oriented | **De-escalation only — reply softly or stay silent. No punishments. No counters.** |
| Hot reload | Web dashboard (no file editing) | Web dashboard | Restart-required for most | **YAML + fsnotify, no UI** |
| Owner control | Web auth | Discord role permissions | Bot admin commands | **Hardcoded owner JIDs, DM-only commands** |
| Anti-detection / "human delays" | Common (and risky — Meta detects them) | N/A (Discord allows bots officially) | N/A (Telegram allows bots officially) | **Refused: low volume *is* the safety, not theatrics** |
| LLM in the loop | Increasingly common; being curtailed by Meta from Jan 2026 | Common (AI toxicity classifiers) | Increasingly common | **Refused: hallucination breaks the calming tone** |
| Leaderboards / engagement gimmicks | Sometimes (gamified upsells) | Very common (XP, ranks, streaks) | Common | **Refused: inverts the framing** |
| Image / OCR matching | Some tools offer it | Some bots offer it | Rare | **Refused: out of scope, privacy + FP cost** |
| Reactions as reply | Rare | Some bots (vote reactions) | N/A | **First-class differentiator (v1.x)** |
| Bot identifies as a bot | Often hidden behind a "human agent" facade | Always visibly a bot | Always visibly a bot | **Transparently a bot — honesty reinforces calm** |

## Sources

- [whatsmeow on pkg.go.dev — capabilities (BuildReaction, BuildPollCreation, BuildPollVote, SendPresence, ContextInfo, ExtendedTextMessage)](https://pkg.go.dev/go.mau.fi/whatsmeow) — HIGH
- [tulir/whatsmeow GitHub — README + send.go + discussions on quoted replies (StanzaId/Participant/QuotedMessage shape)](https://github.com/tulir/whatsmeow) — HIGH
- [whatsmeow discussion #215 — QuotedMessage construction](https://github.com/tulir/whatsmeow/discussions/215) — HIGH
- [whatsmeow discussion #148 / issue #135 — quoting in groups, iOS rendering caveats](https://github.com/tulir/whatsmeow/discussions/148) — HIGH
- [whatsmeow events / types packages](https://pkg.go.dev/go.mau.fi/whatsmeow/types/events) — HIGH
- [Meta's WhatsApp AI Chatbot Ban (MEF, Dec 2025) — general-purpose LLM chatbots prohibited on WhatsApp Business Platform from Jan 15, 2026](https://mobileecosystemforum.com/2025/12/01/metas-whatsapp-ai-chatbot-ban/) — MEDIUM (regulatory headwind reinforcing "no LLM" anti-feature)
- [respond.io — "Not All Chatbots Are Banned: WhatsApp's 2026 AI Policy Explained"](https://respond.io/blog/whatsapp-general-purpose-chatbots-ban) — MEDIUM
- [kraya-ai — WhatsApp automation ban risk; behavioral detection: message velocity, typing-presence patterns, protocol fingerprinting](https://blog.kraya-ai.com/whatsapp-automation-ban-risk) — MEDIUM (informs "anti-detection theatrics don't actually help" reasoning)
- [WASenderApi — anti-ban posture for unofficial APIs](https://wasenderapi.com/blog/stop-getting-banned-the-ultimate-whatsapp-anti-ban-strategy-for-unofficial-apis-in-2025) — MEDIUM
- [Discord AutoMod safety overview — keyword filters, escalation patterns](https://discord.com/safety/auto-moderation-in-discord) — MEDIUM
- [VibeBot moderation features — AI toxicity flagging](https://www.vibebot.gg/features/moderation) — MEDIUM
- [ExpressTech — Discord moderation bot escalation patterns (warn → mute → ban)](https://www.expresstechsoftwares.com/the-ultimate-guide-to-discord-bots-for-moderation-transform-your-community-management/) — MEDIUM (source for "warn ladder" anti-feature reasoning)
- [thoughtbot — per-user chat-bot rate limit strategy](https://thoughtbot.com/blog/chat-bot-per-user-rate-limits) — MEDIUM
- [discord.py rate limiting / commands.cooldown decorator patterns](https://app.studyraid.com/en/read/7183/176818/rate-limiting-and-anti-spam-measures) — MEDIUM (validates per-user-bucket pattern as standard)
- [SleekFlow / Verloop / Chatimize / botpenguin — WhatsApp auto-reply feature surveys](https://sleekflow.io/blog/best-whatsapp-auto-reply) — MEDIUM (competitor feature baseline)
- [influencermarketinghub — 18 WhatsApp chatbot tools 2025](https://influencermarketinghub.com/whatsapp-chatbot-tools/) — MEDIUM
- [Match Data Studio — Levenshtein vs Jaro-Winkler tradeoffs; FP/FN tuning](https://match-data.studio/blog/fuzzy-matching-algorithms-explained/) — MEDIUM (informs "tokenize first, tune distance per matcher" implementation note)
- [Engagement bot landscape (Engagerly, Aki Streaks, ActivityRank)](https://engagerly.bot/) — MEDIUM (concrete examples of leaderboard/streak anti-features camomila explicitly inverts)

---
*Feature research for: WhatsApp single-group de-escalation bot (camomila / whatsmeow / Go)*
*Researched: 2026-05-22*
