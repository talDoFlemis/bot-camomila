# Pitfalls Research

**Domain:** whatsmeow-based single-group reply bot (fuzzy-match, hot-reloaded YAML, SQLite session, Docker, long-running unattended)
**Researched:** 2026-05-22
**Confidence:** HIGH for whatsmeow / fsnotify / SQLite / Docker pitfalls (verified against tulir/whatsmeow issues + fsnotify upstream + SQLite forum). MEDIUM for ban-rate heuristics (real numbers are not published by WhatsApp; community estimates only).

---

## Critical Pitfalls

### Pitfall 1: Account ban / "device removed" from automation fingerprinting

**What goes wrong:**
The paired WhatsApp account is silently logged out — whatsmeow emits `events.LoggedOut` (often with `stream:error code="401"` + `conflict type="device_removed"`) and the SQLite device store is wiped. Reconnection requires a fresh QR scan. In the worst case the *phone number itself* is restricted, not just this device.

**Why it happens:**
- whatsmeow is reverse-engineered (unofficial) WhatsApp Web protocol — WhatsApp actively fingerprints clients.
- Behavioral signals trigger restrictions: high outbound message velocity, replying instantly with millisecond latency, replying to messages from senders not in the contact list, sending identical text repeatedly, sending out of "human" hours.
- The exact thresholds aren't published. Community reports: accounts running unrestrained automation last 2–8 weeks; bots respecting human-paced cadence on a *single group* with low volume can last indefinitely.
- A second process opening the same session triggers `events.StreamReplaced` and can also escalate to bans if it loops.

**How to avoid:**
- **Hard cap outbound rate.** Configurable max-replies-per-minute and per-hour ceilings enforced *before* the cooldown check (so even if cooldown logic has a bug, a global limiter still throttles).
- **Per-matcher cooldown + per-user cooldown + per-group cooldown** stacked (not OR'd). Default values: matcher 5–15min, user 30–60min, group floor 30s between any two replies.
- **Random reply latency** (e.g., 2–8 seconds jitter) instead of instant. *Not* anti-detection theater — just stops the "0ms response time" fingerprint.
- **Quiet hours enforced by wall clock in a fixed TZ** (see Pitfall 8). Do not reply between configured quiet hours.
- **One process per session, ever.** Use a lockfile next to the SQLite DB. On startup, if lock held, refuse to start.
- **Handle `events.LoggedOut` cleanly**: stop, surface a loud signal (exit non-zero, log at ERROR), never auto-pair without operator intervention.
- **Pin a recent whatsmeow version and watch for `405 Client outdated`** (see Pitfall 5).
- Use a **dedicated phone number** for the bot. Never the operator's personal number.

**Warning signs:**
- Logs show `events.Disconnected` reconnect loops more than once per hour.
- `stream:error` events with code 401/403/503 in logs.
- Any `events.LoggedOut` event at all — this is *the* signal, treat as P0.
- Group members ask "did the bot get muted?" → check device list on phone.

**Phase to address:** Phase 1 (session bootstrap — lockfile, LoggedOut handler). Phase 2 (matcher dispatch — global rate limit, jitter). Phase 3 (cooldowns — stacked limits, quiet hours).

---

### Pitfall 2: Replying outside the configured group (scope leak)

**What goes wrong:**
Bot replies in a DM, another group, a status broadcast, or to itself — even though config says single group only. In the worst case, an attacker DMs the bot a trigger word and gets a reply, confirming the bot is alive and revealing matcher behavior.

**Why it happens:**
- whatsmeow's `events.Message` fires for *every* message the account receives: DMs, all groups, broadcasts, the bot's own outbound. No automatic filtering.
- JID server suffixes are easy to confuse: `@s.whatsapp.net` (user), `@g.us` (group), `@lid` (logged-in device, increasingly used in DMs), `@broadcast`, `c.us` (legacy).
- In DMs, the `to` field can be a LID rather than a phone-number JID, so a naive equality check against a configured user JID fails. Same risk in reverse for group filters if the configured group JID format drifts.
- Message-info `Chat` vs `Sender` vs `Recipient` are three different fields. Picking the wrong one to filter on lets messages through.

**How to avoid:**
- **Allow-list, not deny-list.** The dispatcher's first check: `if msg.Info.Chat.String() != cfg.GroupJID { return }`. No exceptions, no fallthrough.
- **Validate the configured JID at startup**: parse with `types.ParseJID`, assert `Server == types.GroupServer` ("g.us"), refuse to start otherwise. Don't trust the YAML.
- **Skip `IsFromMe` early.** Right after the group check, drop messages the bot itself sent. (See Pitfall 6 for why this is a separate concern from echo loops.)
- **Skip system/protocol messages** (`msg.Info.Type != "text"` for v1; revisions, reactions, group-event messages should not trigger).
- **Unit test the dispatcher** with table-driven inputs: DM, wrong group, right group, broadcast, self-message, system message. Every non-matching case must return without sending.
- **Log every send-decision** at INFO (chat JID, matched matcher, decision). When something leaks, the log tells you which check failed.

**Warning signs:**
- Any outbound `SendMessage` call where the destination JID differs from `cfg.GroupJID` — log this at WARN and *abort the send*.
- Reply count > 0 for any chat JID other than the configured group, across any window.
- The bot replies to a DM during operator testing — that's the canary.

**Phase to address:** Phase 2 (message dispatch — JID allow-list, type filter). Phase 1 (config validation — reject non-group JID at load).

---

### Pitfall 3: SQLite session-store corruption on restart / Docker upgrade

**What goes wrong:**
On restart or container upgrade, whatsmeow fails to open its session DB with `SQLITE_CORRUPT`, `file is not a database`, `malformed database schema`, or `database is locked`. The bot can't restart without re-pairing (losing the session). In the worst case, residual `-wal` / `-shm` files from a different sqlite driver version cause silent schema drift.

**Why it happens:**
- **Two processes open the same DB.** Container restarted but old process not fully stopped (or two replicas with shared volume) → both write WAL → corruption. WAL itself works fine across processes on a *shared host kernel* (per Simon Willison's 2026 research), but only one whatsmeow process should ever exist.
- **Volume on a network filesystem** (NFS/CIFS/SMB, some VPS providers' "network block storage"): SQLite POSIX locks degrade or lie. WAL specifically warns against this.
- **Hard kill during write.** `docker kill` (SIGKILL) mid-WAL-checkpoint can leave `-wal` orphaned. SQLite usually recovers, but combined with driver changes (modernc.org/sqlite ↔ mattn/go-sqlite3) the residual journal can break schema-load.
- **Backup taken while bot running** (cp of the .sqlite file alone, ignoring `-wal` and `-shm`) → restored DB is half-stale and corrupt.
- **bind-mount on macOS/Windows Docker Desktop** uses 9p/virtiofs with slower fsync; long lock holds → "database is locked" errors under contention.

**How to avoid:**
- **Single-process invariant**: lockfile (see Pitfall 1). Container restart policy should be `unless-stopped` not `always`, and shouldn't fight a running instance.
- **Graceful shutdown**: handle SIGTERM, call `client.Disconnect()`, then `db.Close()`, *then* exit. Compose/k8s default 10s grace is enough; don't override to 0.
- **Pick one SQLite driver and stick with it.** `modernc.org/sqlite` (pure Go, no CGO) is the simpler default for this project — avoids the Alpine/musl CGO morass (see Pitfall 9). Switching drivers later requires a migration.
- **Volume on the host's native filesystem.** Named Docker volume on ext4/xfs. Refuse to deploy to NFS-backed storage; document this.
- **Backups must include `.sqlite`, `-wal`, and `-shm`** or use `sqlite3 .backup` (online backup API). Better: stop the bot, copy, start.
- **At startup, run `PRAGMA integrity_check`** on the session DB before handing it to whatsmeow. On failure, refuse to start (don't auto-wipe — operator decides).
- **Pin the SQLite driver version** in `go.mod`. Treat driver upgrades like protocol-version upgrades: deliberate, with a fresh QR pairing as fallback.

**Warning signs:**
- Stray `*.sqlite-wal` files older than a few seconds while the bot is supposedly stopped.
- `database is locked` in logs more than once at startup.
- File size of `-wal` growing without bound (checkpointing failing).
- Container restart count climbing without the bot's logs showing clean shutdown messages.

**Phase to address:** Phase 1 (session storage — lockfile, driver choice, integrity check, graceful shutdown). Phase 6 (deployment — Docker volume guidance, backup procedure).

---

### Pitfall 4: Hot-reload races and "stuck-old-config" bugs (fsnotify)

**What goes wrong:**
- Editor saves YAML → bot reads it mid-write → YAML parse fails → bot keeps running on old config silently. Operator thinks change applied; it didn't.
- Editor uses atomic-rename save (vim's `:w`, VS Code's default, `mv tmp config.yaml`) → original inode disappears → fsnotify watcher on the file silently stops firing. Subsequent saves are *invisible*. Bot frozen on whatever config was loaded last.
- A burst of 3–5 events per save (CREATE, WRITE, WRITE, CHMOD, RENAME) triggers 3–5 reloads in milliseconds → reply-storm: matchers swap mid-dispatch, cooldowns reset, the same trigger fires 5 times.
- Reload happens *during* a `SendMessage`-pending dispatch using captured-by-reference matchers → goroutine reads partially-updated state, dispatches with mixed-version config.

**How to avoid:**
- **Watch the parent directory, not the file.** This is the upstream fsnotify recommendation (issue #17, #214, #372). Filter events by name.
- **Debounce** with a small window (250–500ms). Coalesce bursts into one reload.
- **Two-stage validation**: parse + validate YAML into a *new* `Config` value. Only swap the live pointer atomically (`atomic.Pointer[Config]` or under a `sync.RWMutex`) *after* validation passes. On parse/validate error, keep old config and log at WARN — never crash, never silently zero out matchers.
- **Copy-on-read in the dispatcher**: at the top of each message handler, take one snapshot of the current `*Config` and use that snapshot for the whole dispatch. Don't re-read mid-handler.
- **Stat the file periodically (every 30–60s) as a belt-and-suspenders check.** If mtime changed but no fsnotify event fired (atomic-rename re-watch failure), reload anyway. Cheap insurance.
- **Don't reset cooldowns on reload.** Cooldown state lives outside `*Config`. Reloading matchers must not zero per-matcher/per-user timers — otherwise reload itself becomes a spam vector.
- **Log every reload outcome** (`reload ok matchers=4`, `reload failed err=...`) at INFO. Operators need to *see* whether the change took.

**Warning signs:**
- `editor saves file, no log line about reload appears` → atomic-rename re-watch failure.
- "I changed the config but the bot still uses the old matcher" → silent parse failure or stale snapshot.
- Multiple replies fire to the same trigger immediately after a save → reload-storm + cooldown reset.

**Phase to address:** Phase 4 (config hot-reload — parent-dir watch, debounce, atomic swap, mtime fallback). Phase 3 (cooldown state — kept outside Config so reloads don't zero it).

---

### Pitfall 5: whatsmeow protocol-version drift → silent or sudden lockout

**What goes wrong:**
WhatsApp ships a new minimum-required client version. The bot's pinned whatsmeow either:
- Refuses to connect with `405 Client outdated`. Bot is dead until upgraded.
- Connects but gets immediately logged out as `device_removed`. Bot is dead until upgraded *and* re-paired.

**Why it happens:**
- whatsmeow's own version constant is bumped by upstream commits, not by SemVer releases. A `go get -u` pulls fresh commits with API changes (e.g., `GetGroupInfo` adding a `context.Context` parameter recently — that's a hard compile break and a hard runtime break).
- Pinning by tag is unreliable because whatsmeow ships from `main`; the version constant lives in the source.
- Upgrades sometimes change the appstate schema or session-store schema. Mid-upgrade restart → corruption (Pitfall 3).

**How to avoid:**
- **Pin to a specific commit SHA in `go.mod`**, not `latest`, not a tag. Document the commit date.
- **Subscribe to whatsmeow's GitHub releases / issues** (or set up a monthly cron-style reminder). Bumping whatsmeow is operational work, not "drive-by `go get`".
- **Before upgrading**: read the diff for API changes (search for new required parameters), test in a sandbox group with a throwaway number.
- **On startup, log the whatsmeow commit SHA** and the WhatsApp protocol version it advertises. Operator can correlate against incidents.
- **Handle `events.ClientOutdated` / 405 explicitly**: log at ERROR, exit non-zero. Don't loop reconnect attempts (looks like abuse → ban risk per Pitfall 1).
- **Treat upgrade as a phased exercise**: stop bot → backup session DB → upgrade → start → watch logs → verify pairing intact. Not a rolling deploy.

**Warning signs:**
- `405` or `client outdated` anywhere in logs.
- Connect succeeds but `events.LoggedOut` with `device_removed` follows within seconds.
- A new whatsmeow commit changed function signatures in your code paths → build break is the warning.

**Phase to address:** Phase 1 (dependency pinning). Phase 7+ (maintenance runbook).

---

### Pitfall 6: Reply-to-self loops and self-message echo

**What goes wrong:**
The bot's own reply contains a configured keyword (e.g., the reply mentions "SEFAZ" to acknowledge it) → the message arrives back as an `events.Message` → matcher fires → bot replies to its own reply → instant infinite loop until cooldown or ban.

**Why it happens:**
- whatsmeow delivers `events.Message` for messages sent *by the bot's own session* too. `Info.IsFromMe == true`.
- It's tempting to write the matcher so it checks the message body uniformly — and the bot's reply text *is* a message body.
- Multi-device WhatsApp: a message can have a different sender suffix on web (`xxxxxxxxxx:NN@s.whatsapp.net`) vs mobile (`xxxxxxxxxx@s.whatsapp.net`). A naive "is this me?" check on `Sender` string equality fails to suppress the loop.
- "Quoted message" matching (per PROJECT.md: matcher checks quoted text too) means if the bot's reply quotes the trigger, and someone *quotes the bot's reply*, the keyword appears in quoted text again — chain loop.

**How to avoid:**
- **First filter in the dispatcher: `if msg.Info.IsFromMe { return }`.** Before any matcher logic. Before cooldown lookup. Before logging at INFO. This is the canonical whatsmeow guard.
- **Belt-and-suspenders**: also drop messages whose `Sender.User` (just the digits, suffix stripped) equals the bot's own number. Catches edge cases where `IsFromMe` is mis-set by upstream protocol changes.
- **When matching quoted text, skip if the quoted message's author is the bot itself.** This breaks the "quote-chain" loop.
- **Curate `answers` to not contain trigger words.** Concrete: write a startup-time check that scans every answer in every cluster for any matcher keyword within that matcher's `distance`. Refuse to start if hit (or warn loudly + skip those matchers).
- **Don't suppress `IsFromMe` even briefly for "testing".** If you need to test, send from a different account.

**Warning signs:**
- Two replies in a row with the same content, threaded to each other.
- Reply count spikes after a reply, not after a user message.
- `IsFromMe == true` rows in the dispatch log (means the guard is missing or ordered wrong).

**Phase to address:** Phase 2 (dispatcher — `IsFromMe` early-return). Phase 1 (config validation — startup check: no answer contains its own matcher trigger within distance).

---

### Pitfall 7: Cooldown drift on long uptimes (wall-clock vs monotonic)

**What goes wrong:**
After weeks of uptime, cooldowns either fire too often or never fire. Specifically:
- NTP adjusts the system clock back by N seconds → cooldown "deadline" is now in the future by extra N seconds → user gets stuck in cooldown longer than configured.
- NTP jumps clock forward → deadline already passed → "5-minute cooldown" only lasts 30 seconds → spam window.
- Daylight-saving transitions, manual VPS clock corrections, or the VPS host hibernating/migrating → larger jumps.
- The host sleeps/suspends → on some kernels the *monotonic* clock pauses too, causing the opposite bug (cooldown shorter than intended because process-time froze).

**Why it happens:**
- Naive cooldowns store `time.Now()` and compare with `time.Now()` later. Go's `time.Time` *does* carry a monotonic reading for the `Sub()` operation — but only if you compare via subtraction and the value was created by `time.Now()` (not by deserialization, not by `time.Unix`).
- Storing `lastTriggered.Unix()` (as int64 seconds) for serialization or logging strips the monotonic part → comparisons fall back to wall-clock → drift.
- Many tutorials show `if time.Since(last) > cooldown` which *does* use monotonic — but `time.Since(last)` after a `last = time.Unix(x, 0)` round-trip silently degrades.

**How to avoid:**
- **Use `time.Since(last)` for cooldown checks**, where `last` is the unmodified `time.Time` returned by `time.Now()`. Don't round-trip through Unix seconds.
- **Keep cooldown state purely in memory** (PROJECT.md already commits to this for v1 — preserves monotonic part automatically).
- **For *quiet hours*, do the opposite — explicitly use wall-clock in a fixed TZ** (see Pitfall 8). Quiet hours are calendar facts, not durations.
- **Document the suspend behavior**: on VPS migration / host pause, cooldowns may misbehave for ~one window. Acceptable for v1; flag in operator docs.
- **Unit test with `clock` abstraction** (e.g., `github.com/benbjohnson/clock`) so cooldown logic can be tested without sleeping real seconds.

**Warning signs:**
- After an NTP sync log entry from the host, a burst of replies that "shouldn't have been allowed" appears.
- After weeks of uptime, replies fire on every trigger (cooldown effectively zero) or never fire (cooldown effectively forever).
- Cooldown durations in logs that don't match `cooldown_seconds` in config.

**Phase to address:** Phase 3 (cooldowns — `time.Since` discipline, in-memory only, clock-abstraction tests).

---

### Pitfall 8: Quiet hours wrong because of container timezone

**What goes wrong:**
Quiet hours configured as `22:00–08:00` are evaluated against UTC inside the container while the operator thinks they're local (e.g., America/Sao_Paulo, UTC-3). Bot is silent from 19:00–05:00 local instead of 22:00–08:00 → fires during the actual quiet window, silent during peak hours.

**Why it happens:**
- Alpine base images **do not include `tzdata`**. Setting `TZ=America/Sao_Paulo` in compose has no effect because `/usr/share/zoneinfo/America/Sao_Paulo` doesn't exist.
- `time.LoadLocation("America/Sao_Paulo")` then returns an error, and lazy code falls back to `time.Local` which is UTC inside the container.
- Even with `tzdata` installed, scratch/distroless images may need explicit `ENV TZ=...` + zoneinfo file copy.
- Go's `time.Local` reads `/etc/localtime` at init time; setting `TZ` later in the process doesn't always re-resolve.

**How to avoid:**
- **Make timezone an explicit config field**, e.g., `timezone: America/Sao_Paulo`. Don't rely on `time.Local`. Don't rely on `TZ` env var alone.
- **Call `time.LoadLocation(cfg.Timezone)` at startup and fail loudly on error.** Pass that `*time.Location` everywhere quiet-hours logic runs.
- **Bake `tzdata` into the Docker image.** For Alpine: `RUN apk add --no-cache tzdata`. For distroless: copy `/usr/share/zoneinfo` from a builder stage.
- **Log the resolved timezone + current local time at startup.** Operator sees immediately if it's wrong: `quiet_hours tz=America/Sao_Paulo now=2026-05-22T14:30-03:00`.
- **Unit-test quiet-hours logic with crafted `time.Time` values in different zones**, including DST transition days.
- **Locale for unicode**: not a quiet-hours concern, but for the same reason (musl Alpine quirks), ensure `golang.org/x/text/unicode/norm` is used in the matcher (see Pitfall 11). Don't rely on the container's locale.

**Warning signs:**
- Bot replies at 3am or stays silent at 3pm.
- Startup logs show `Local` as timezone instead of an IANA name.
- `time.LoadLocation` returns `unknown time zone` errors.

**Phase to address:** Phase 3 (quiet hours — config-driven timezone, `tzdata` in image). Phase 6 (Dockerfile — base image + tzdata).

---

### Pitfall 9: Docker base-image choice breaks whatsmeow / SQLite

**What goes wrong:**
The Go binary built on Debian-slim runs fine locally, but on Alpine it dies with `Error loading shared libraries: ... not found` (musl vs glibc), or whatsmeow's SQLite session-store fails to open because the bundled CGO SQLite can't link, or the binary works but every WhatsApp message with an emoji or accented character fails to match because no unicode locale data is present.

**Why it happens:**
- Alpine uses **musl libc**. A binary built against glibc (Debian, Ubuntu, default `golang` image) crashes on Alpine. The reverse usually works but isn't free.
- `mattn/go-sqlite3` is CGO and links libsqlite — must be built on the same libc as the runtime image. Multi-stage `golang:alpine` builder → `alpine` runtime: needs `CGO_ENABLED=1`, `apk add build-base sqlite-dev`, careful linker flags.
- `modernc.org/sqlite` is pure Go (no CGO), so it sidesteps the libc issue entirely. **For this project, this is the right default** — PROJECT.md already lists it as an option.
- Scratch/distroless images lack `tzdata` (Pitfall 8) and `ca-certificates` (whatsmeow needs HTTPS to WhatsApp servers — without CA bundle, every connect fails with x509 errors).
- Distroless `:static` skips even more (no DNS resolver in some configs).

**How to avoid:**
- **Default base: `gcr.io/distroless/static-debian12` or `alpine:3.20` with `tzdata` + `ca-certificates` installed.** Avoid `scratch` unless explicitly tested.
- **Default SQLite driver: `modernc.org/sqlite` (pure Go)**, build with `CGO_ENABLED=0`. Simpler Dockerfile, smaller image, no libc games.
- **Multi-stage build**: `golang:1.26-bookworm` (or `alpine`) builder → minimal runtime. Builder copies only the binary + needed runtime files (zoneinfo, CA bundle).
- **In runtime image, always include**: `ca-certificates`, `tzdata`. Alpine: `apk add --no-cache ca-certificates tzdata`. Distroless static: already bundled.
- **Smoke test the image** before deploy: run it in a throwaway container, watch it complete QR pairing and one round-trip message. Catches libc/CA/TZ issues before production.
- **Document the chosen base image** in the Dockerfile header. If someone switches to `scratch` for size, they need to add tzdata + CA bundle deliberately.

**Warning signs:**
- `x509: certificate signed by unknown authority` on connect → missing CA bundle.
- `unknown time zone` on startup → missing tzdata.
- `no such file or directory` for the binary itself in container logs → libc mismatch.
- "Works on my machine, dies in Docker" → almost always one of the above.

**Phase to address:** Phase 6 (Dockerfile — base image choice, build flags, included packages). Phase 1 (driver choice — `modernc.org/sqlite` decision codified).

---

### Pitfall 10: Levenshtein distance:1 false positives on short words

**What goes wrong:**
The example config matches `SEFAZ` with `distance: 1` — that matches `SEFAZ`, `SEFAS`, `SERAZ`, `SEFAR`, `XEFAZ`, `EFAZ`, `SEFAZX`, etc. Now apply the same matcher to a shorter word: `RJ` with `distance: 1` matches almost every two-letter token (`RI`, `AJ`, `J`, `R`, `RIO`...). Bot replies to noise constantly, gets rate-limited (Pitfall 1), bans risk skyrockets.

**Why it happens:**
- Levenshtein is an absolute count, not a ratio. For an N-character word, distance 1 = (1/N) edit ratio. At N=3, that's 33% mismatch tolerance.
- WhatsApp messages contain emoji, punctuation, casing variants, and accented characters that count as "different characters" naively, inflating Levenshtein distance unless you normalize.
- Matchers run on the *entire message body* — naive substring matching with Levenshtein at distance 1 against any token in a long message means a 200-char rant has ~200 chances to fuzzily hit something.
- "Distance against the message" vs "distance against each token of the message" vs "best-match across tokens" are three different algorithms; mixing them gives surprises.

**How to avoid:**
- **Tokenize the message first** (whitespace + punctuation + emoji boundaries), then match per-token. Don't slide Levenshtein across the entire string.
- **Enforce minimum word length per distance** at config-load time:
  - distance 0 (exact match) — any length.
  - distance 1 — minimum word length 5 characters.
  - distance 2 — minimum 8 characters.
  - distance ≥3 — discouraged; require explicit acknowledgement in config (`force_long_distance: true`).
- **Reject ambiguous matchers at startup**. For each matcher, generate the set of strings within edit distance, compare against a small lexicon (or just the bot's own answers — see Pitfall 6). Warn if a matcher would match common Portuguese words.
- **Normalize before matching** (see Pitfall 11): lowercase, Unicode NFC, strip diacritics, strip punctuation/emoji.
- **Anchor matchers to whole tokens**: distance 1 against `SEFAZ` should match the token `SEFAS` but not the token `SEFAZESES` (substring) unless `substring: true` is opted in.
- **Per-matcher unit tests in the config repo**: each matcher ships with a list of "must match" and "must not match" example strings. CI runs them. Refactoring matchers can't regress silently.

**Warning signs:**
- Cooldown saving the bot from itself: lots of "matched but in cooldown" log lines for the same matcher in a short window → matcher is too greedy.
- Group members report bot replied to a message they didn't think was about the topic.
- Matcher fires more than ~1× per real-world trigger event per day.

**Phase to address:** Phase 2 (matcher engine — tokenization, min-length enforcement, anchored matching). Phase 1 (config validation — reject ambiguous matchers, require per-matcher example tests).

---

### Pitfall 11: Unicode-normalization mismatch (accents, emoji, case)

**What goes wrong:**
A configured matcher word `café` is stored in YAML as NFC (`c-a-f-é` with precomposed é, U+00E9). A user sends `café` typed on iOS, which sometimes produces NFD (`c-a-f-e-´` with combining acute, U+0301). String comparison fails. Levenshtein distance between them is *not zero* (it's 1 — one combining character difference) and not the "1 typo" the operator intended. Configured `distance: 1` matches *both* the typo'd `cafe` *and* the NFD `café` — surprising hit, surprising miss.

**Why it happens:**
- Unicode has multiple canonical encodings for the same visual character. WhatsApp does not normalize on send; whichever form the sender's keyboard produced is what arrives.
- Go's `strings.EqualFold` and `==` operate on bytes/runes, not normalized characters.
- macOS filesystem returns NFD; iOS keyboards sometimes too. Most Linux/Android: NFC. Web WhatsApp: depends on the browser.
- Emoji modifiers (skin tone, gender), variation selectors (VS16 makes a base emoji "colorful"), and ZWJ sequences look like extra characters to Levenshtein.

**How to avoid:**
- **Normalize both sides to NFC before matching.** Use `golang.org/x/text/unicode/norm`: `norm.NFC.String(s)`.
- **Lowercase after normalization** using `golang.org/x/text/cases` (locale-aware) or at minimum `strings.ToLower` for ASCII-dominant Portuguese keywords.
- **Strip diacritics for matching only** (not for display) by decomposing to NFD and removing combining marks. Helps `cafe`/`café` collide intentionally. Document this as a per-matcher option (`strip_accents: true`) — not all matchers want it.
- **Strip emoji / variation selectors before length-checking** (Pitfall 10's min-length rule should count "real" characters, not modifier runes).
- **Test fixtures must include NFC + NFD versions** of every accented matcher keyword and at least one emoji-laced message.
- **Don't normalize in the matcher hot-path uniquely each call** — normalize once on config load (for keywords) and once per message (for the body). Cache the normalized message during dispatch.

**Warning signs:**
- "It matches sometimes but not always" reports → almost always normalization.
- Matchers with accented keywords (`não`, `está`, `você`) fire less often than expected.
- Distance counts in debug logs that are 1 higher than they "should" be → combining-mark drift.

**Phase to address:** Phase 2 (matcher engine — normalization pipeline). Phase 1 (config validation — normalize keywords on load).

---

### Pitfall 12: Kill switch ignored or delayed

**What goes wrong:**
Operator DMs the kill-switch command. Nothing happens, or the bot pauses 30 minutes later, or the bot pauses then resumes itself on next reload. Group continues getting replies during an incident.

**Why it happens:**
- DMs from owner hit the dispatcher *after* the group-allow-list filter (Pitfall 2) and get dropped before the command parser sees them.
- Command parsing checks group membership / chat type first, ignoring DM-from-owner.
- Pause state stored as a field on `Config` → next config hot-reload re-reads YAML → pause flag reset to false (Pitfall 4).
- Pause state stored only in memory but checked *after* matcher dispatch decision, only suppressing the send. Cooldown still ticks, telemetry still says "would have sent".

**How to avoid:**
- **Owner DMs are a separate code path from group matchers.** Route by: `is this from an owner JID in DM? → command parser. is this from the configured group? → matcher dispatch. else: drop.`
- **Pause state lives outside `Config`.** A separate `Runtime` struct or `atomic.Bool` that hot-reload does not touch.
- **Pause check is the *first* gate** in matcher dispatch, before cooldown lookup, before matching, before logging. Paused = early return after ack-log.
- **Acknowledge the kill command** by sending an emoji reaction or DM reply, so the operator sees it landed. Silent kill-switch erodes operator trust during incidents.
- **Survives restart? V1 decision needed.** Easiest v1: pause state is in-memory, restart = unpause (and operator can re-pause). If pause must survive restart, persist to a small `state.json` next to the SQLite DB. Document the choice.
- **Test the kill switch in CI / integration**: simulate owner DM → assert dispatcher's pause flag is true → simulate group message → assert no send.

**Warning signs:**
- Operator says "I sent /pause and it kept replying."
- After a `reload` event, the bot starts replying again on its own.
- No log line acknowledging owner commands.

**Phase to address:** Phase 5 (owner commands + kill switch — owner DM path, pause-first gate, ack). Phase 4 (hot-reload — Runtime state separate from Config).

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Cooldown state purely in-memory (no persistence) | Simple v1, no DB schema, no migration | Restart resets all cooldowns → potential spam window at restart, especially if bot crash-loops | v1 (per PROJECT.md). Add persistence in v2 if restart-frequency becomes high. |
| Single SQLite DB shared between whatsmeow session and (eventually) app state | One file to back up | Schema collisions, harder to upgrade whatsmeow independently, locking contention | Acceptable while *only* whatsmeow tables exist. Switch to separate DB the moment app tables are added. |
| Hardcoded owner JIDs in YAML (no rotation flow) | Simplest auth | If an owner phone gets stolen, only redeploy fixes it | Acceptable for personal-use v1 (per PROJECT.md). Never for shared deployments. |
| No outbound message archival | No PII storage, faster, simpler | Can't audit a "did the bot really say that?" complaint | Acceptable. Compensate with INFO-level structured logs of every send decision. |
| Pure-Go `modernc.org/sqlite` instead of CGO `mattn/go-sqlite3` | Trivial Docker build, smaller image, no libc games | ~2–5x slower for heavy workloads | Acceptable forever for a single-group bot with low write volume. Re-evaluate only if profiling shows SQLite as hot path (it won't). |
| Static random reply latency (e.g., always 3s) instead of jittered | Predictable, easy to test | Trivially fingerprintable by WhatsApp; defeats the anti-fingerprint purpose | Never. If you add a delay, jitter it. |
| Bind-mount instead of named volume for SQLite | Easy to inspect from host | On macOS/Windows Docker Desktop, 9p/virtiofs causes lock issues (Pitfall 3) | Linux-only deployments. Never for cross-platform images. |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| whatsmeow event handlers | Registering one handler that does dispatch + logging + state mutation; panics take down the whole event loop | Wrap handler logic in `defer recover()`; do dispatch in a goroutine with bounded concurrency; log panics at ERROR |
| whatsmeow `client.SendMessage` | Calling without `context.Context` deadline → blocks forever on network stall | Always pass `ctx` with a 30s timeout; on timeout, log and let cooldown protect against retry-storm |
| whatsmeow JID parsing | Trusting strings from config; comparing JID strings instead of structured fields | `types.ParseJID()` at startup, assert `.Server == types.GroupServer`, compare via `.String()` only for equality of fully-normalized values |
| fsnotify on Linux | Watching the file (not the directory) → atomic-rename saves silently break the watch | Watch the directory containing config; filter events by filename |
| fsnotify on Docker bind-mount | inotify events sometimes don't propagate from host edits into container (macOS/Windows Docker Desktop) | Combine fsnotify with a periodic `stat` poll (30–60s) as fallback. On Linux native Docker, fsnotify alone works. |
| SQLite + Docker volumes | Putting the volume on networked storage (NFS, "block storage" on some VPS providers) | Use a named volume on the host's local ext4/xfs filesystem; explicitly document this in deploy docs |
| Docker container TZ | Relying on `TZ` env var without installing `tzdata` (Alpine) | `apk add --no-cache tzdata`; explicit `time.LoadLocation` in code; do not rely on `time.Local` |
| Docker container TLS | Building on scratch without CA bundle → x509 errors on every whatsmeow connect | Include `ca-certificates` package; or use `gcr.io/distroless/static-debian12` which bundles it |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Sliding Levenshtein across the entire message body per matcher per message | CPU spikes when long messages arrive in active group | Tokenize first, match per-token, cap message length considered (e.g., first 2000 chars) | Active group with ≥10 matchers and long-form messages (PROJECT.md says ≤10 matchers — borderline) |
| History sync on first pairing in a busy group | First-launch memory spike, possible OOM in tight container | Disable history sync (whatsmeow option) — bot doesn't need history; only new messages matter | Groups with thousands of historical messages |
| Cooldown map growing unbounded (per-user cooldown keyed by JID, never cleaned) | Slow memory growth over weeks | Periodic janitor goroutine: delete entries whose deadline passed >1h ago | Long uptime + active group with many distinct senders |
| WAL file growing without auto-checkpoint | SQLite file size balloons | Default whatsmeow + SQLite auto-checkpoints, but verify; set `PRAGMA wal_autocheckpoint=1000` if needed | Long uptime without app-side writes; usually not an issue here |
| Logging every received message at INFO | Log volume overwhelms disk/log-aggregator | Log received at DEBUG, dispatch-decisions at INFO, sends + errors at WARN/ERROR | Active group, default log level INFO, small VPS disk |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Storing the SQLite session file in a world-readable volume | Session theft = attacker can impersonate the bot account, read all its messages, send as it | Volume permissions `0700`, owner = bot UID; never commit `.sqlite` to git |
| Logging full message bodies at INFO/DEBUG | Privacy leak (group members' messages in plain logs); GDPR-shaped concerns even for personal use | Log message hash + matcher hit, not body. Body only at TRACE level, off by default. |
| Owner JID list parsed loosely (substring, prefix match) | An attacker who can DM the bot with a number containing the owner's digits can trigger kill switch | Exact-string JID comparison after `types.ParseJID` normalization |
| Kill-switch command via group message instead of DM | Anyone in the group can pause the bot | Owner commands DM-only; reject command-shaped messages in the configured group |
| Hot-reload reading a file the operator doesn't intend to be config | Symlink swap → bot loads attacker-controlled YAML → matcher list manipulated | Resolve config path to absolute, refuse to follow symlinks, log the resolved path on every reload |
| Exposing whatsmeow's HTTP debug endpoints (if enabled) on a public port | Remote control of the bot | Never bind whatsmeow's optional debug server; if used, bind to `127.0.0.1` only |
| Including the session SQLite in a Docker image | Image push → session leaked publicly | `.dockerignore` excludes `*.sqlite*`; CI sanity-check refuses to build if found |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Bot replies in-thread but mentions the wrong user (`{REPLIED_USER}` resolves to the bot itself) | Replies feel broken / accusatory | Resolve `{REPLIED_USER}` against the *triggering message's sender*, not against the message the bot is quoting |
| Reply takes 0ms (instant) | Group members notice "bot energy", feels intrusive; also fingerprintable (Pitfall 1) | Jittered 2–8s delay; reply via threaded quote so the context is obvious even with delay |
| Cooldown silently swallows triggers | User repeats the trigger thinking the bot is broken | Acceptable for de-escalation use case (cooldown is the point). But log it; don't reply with "you're in cooldown" — that's worse. |
| Quiet hours don't apply to owner commands | Owner DMs `/status` at 2am, gets a reply with surprising tone | Owner commands bypass quiet hours; they're not group-facing |
| Answer text mentions trigger word | Self-reply loop risk (Pitfall 6) *and* confusing for users (bot escalates by repeating the word) | Curate answers to acknowledge the topic without echoing the trigger verbatim |
| Random-pick answer can repeat the same answer multiple times in a row | Feels less "varied" than promised | v1: acceptable (truly random). v2: weighted random or last-N exclusion |

## "Looks Done But Isn't" Checklist

- [ ] **QR pairing flow:** Often missing — what happens if the QR expires during scan? Verify the bot prints a fresh QR and doesn't crash. Verify session persists across `docker compose restart`.
- [ ] **Group JID validation:** Often missing — verify the bot refuses to start if `group_jid` is a user JID or malformed. Verify it refuses to start if the configured group doesn't exist.
- [ ] **`IsFromMe` filter:** Often missing or in the wrong order — verify by sending a message from the *bot's* phone and confirming no reply.
- [ ] **Hot-reload survives editor atomic save:** Verify by editing config in vim with `:w`, in VS Code, and via `mv tmp config.yaml`. All three must trigger a reload event in logs.
- [ ] **Hot-reload validation:** Verify by writing invalid YAML — bot must log error and keep old config, not crash and not silently zero matchers.
- [ ] **Cooldown survives clock jump:** Verify by `date -s` (in a throwaway VM) and asserting cooldowns behave per `time.Since` semantics.
- [ ] **Quiet hours:** Verify by setting quiet hours to "right now" in the bot's TZ — bot must not reply. Verify by ticking the system clock past the window.
- [ ] **Kill switch:** Verify owner DM `/pause` stops replies within one second. Verify hot-reload after pause does NOT unpause.
- [ ] **Graceful shutdown:** Verify `docker stop` (SIGTERM) closes the SQLite DB cleanly. Check `-wal` file is gone or empty after stop.
- [ ] **Levenshtein on short words:** Verify config-load rejects `distance: 1` with a 2-character keyword.
- [ ] **Unicode normalization:** Verify a matcher keyword with `é` matches both NFC and NFD message inputs.
- [ ] **Docker image TZ:** Verify `docker run image date` shows the configured TZ, not UTC.
- [ ] **Docker image TLS:** Verify the bot connects to WhatsApp on first run without x509 errors.
- [ ] **No accidental scope leak:** Verify by sending a trigger word from DM and from a non-configured group — bot must not reply in either.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Account banned (Pitfall 1) | HIGH | Bot is dead. Procure new phone number, deploy fresh, re-pair, re-add to group. Audit triggers/cooldowns before re-enabling. |
| Scope leak (Pitfall 2) | LOW once detected | Add the missing JID check; redeploy. No data to clean up. |
| SQLite corruption (Pitfall 3) | MEDIUM | Stop bot. Run `sqlite3 session.db ".recover" | sqlite3 recovered.db`. If recovery fails, delete session DB, restart, re-pair via QR. Inform operator that re-pairing means losing currently-active "logged in devices" slot on the phone. |
| Reload-storm (Pitfall 4) | LOW | Add debounce + atomic swap; redeploy. In-flight excess messages already sent — apologize in-group manually. |
| Protocol-version lockout (Pitfall 5) | MEDIUM | Bump whatsmeow to latest commit, rebuild, redeploy. If session got wiped, re-pair. Inspect API for breaking changes (e.g., new ctx params). |
| Self-reply loop (Pitfall 6) | MEDIUM (because cooldown limits damage) | Send /pause immediately. Patch the missing `IsFromMe` guard. Audit answers for self-trigger overlap. Redeploy. |
| Cooldown drift (Pitfall 7) | LOW | Restart bot (in-memory cooldown resets). Patch code to use `time.Since`. |
| Wrong timezone (Pitfall 8) | LOW | Fix Dockerfile (`apk add tzdata`) + config (`timezone: ...`). Rebuild + redeploy. |
| Docker image broken (Pitfall 9) | LOW | Fix Dockerfile. Local smoke test before redeploy. Image-level problems don't corrupt data. |
| Levenshtein false positives (Pitfall 10) | LOW | Bump `distance` down or word longer; reload config (no restart needed if hot-reload works). |
| Unicode mismatch (Pitfall 11) | LOW | Add normalization to matcher; reload. |
| Kill-switch broken (Pitfall 12) | HIGH during incident | Stop the container (`docker stop`). Fix the routing. Until fixed, kill = stop the container. |

## Pitfall-to-Phase Mapping

Phases below are descriptive (what each addresses) — roadmapper will set names/numbers.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1. Ban / device removed | Session bootstrap (lockfile, LoggedOut handler) + Cooldowns/quiet hours + Matcher dispatch (rate limit, jitter) | Manual: run with low limits, monitor `events.Disconnected` / `LoggedOut` over 2 weeks |
| 2. Scope leak | Config validation (Phase 1) + Matcher dispatch (Phase 2) | Unit test dispatcher with DM/wrong-group/right-group/broadcast/self/system message inputs |
| 3. SQLite corruption | Session storage (Phase 1) + Deployment (Phase 6 — volume guidance, backup proc) | `PRAGMA integrity_check` on startup; manual restart-stress test |
| 4. Hot-reload races | Config hot-reload (Phase 4) | Editor save tests (vim, VS Code, mv); invalid-YAML test; storm test (10 rapid saves) |
| 5. whatsmeow protocol drift | Dependency pinning (Phase 1) + Maintenance runbook (post-MVP) | Logged whatsmeow commit SHA at startup; 405-handler integration test |
| 6. Self-reply loop | Matcher dispatch (Phase 2 — `IsFromMe` first) + Config validation (Phase 1 — answer/trigger overlap check) | Send message from bot's own phone → assert no reply |
| 7. Cooldown drift | Cooldowns (Phase 3) | `clock` abstraction unit tests covering forward/backward jumps |
| 8. Quiet hours / TZ | Quiet hours (Phase 3) + Dockerfile (Phase 6) | Startup logs show resolved TZ + local time; CI test with `TZ=` override |
| 9. Docker base image | Dockerfile (Phase 6) | Smoke-test container: QR pair, x509-clean connect, `date` shows correct TZ |
| 10. Levenshtein false positives | Matcher engine (Phase 2) + Config validation (Phase 1) | Per-matcher must-match / must-not-match example tests in repo |
| 11. Unicode normalization | Matcher engine (Phase 2) | Test fixtures with NFC + NFD + emoji versions of every accented matcher keyword |
| 12. Kill switch | Owner commands (Phase 5) + Hot-reload (Phase 4 — Runtime outside Config) | Integration test: DM `/pause` → assert no group sends; trigger reload → still paused |

## Sources

- [WhatsApp ban warnings affecting whatsmeow clients (issue #810)](https://github.com/tulir/whatsmeow/issues/810)
- [Logged out for using unofficial app (issue #561)](https://github.com/tulir/whatsmeow/issues/561)
- [WhatsApp ban risk discussion #199](https://github.com/tulir/whatsmeow/discussions/199)
- [WhatsApp message-automation ban rules update (#567)](https://github.com/tulir/whatsmeow/discussions/567)
- [whatsmeow events package docs](https://pkg.go.dev/go.mau.fi/whatsmeow/types/events) — `IsFromMe`, `StreamReplaced`, `LoggedOut`, `ClientOutdated`
- [whatsmeow types.JID docs](https://pkg.go.dev/go.mau.fi/whatsmeow/types) — JID server suffixes
- [whatsmeow self-message handling (#388)](https://github.com/tulir/whatsmeow/discussions/388)
- [whatsmeow web vs mobile sender format (#568)](https://github.com/tulir/whatsmeow/discussions/568)
- [whatsmeow `Client outdated` 405 errors (whatsapp-mcp #216)](https://github.com/lharries/whatsapp-mcp/issues/216)
- [whatsmeow API breaking change: GetGroupInfo ctx parameter (#153)](https://github.com/lharries/whatsapp-mcp/issues/153)
- [whatsmeow `device_removed` stream:error 401 (#807)](https://github.com/tulir/whatsmeow/issues/807)
- [fsnotify atomic-rename pitfall (issue #17)](https://github.com/fsnotify/fsnotify/issues/17)
- [fsnotify robust file watching (issue #372)](https://github.com/fsnotify/fsnotify/issues/372)
- [fsnotify keep watching on rename (issue #214)](https://github.com/fsnotify/fsnotify/issues/214)
- [fsnotify package docs](https://pkg.go.dev/github.com/fsnotify/fsnotify)
- [SQLite WAL mode across Docker containers (Simon Willison, 2026)](https://simonwillison.net/2026/Apr/7/sqlite-wal-docker-containers/)
- [SQLite file-locking and concurrency](https://sqlite.org/lockingv3.html)
- [SQLite-corruption recovery patterns](https://nickgeorge.net/sqlite-debugging-corruption/)
- [Go monotonic clock semantics (Go issue #12914)](https://github.com/golang/go/issues/12914)
- [Go time wall/monotonic drift (Go issue #27090)](https://github.com/golang/go/issues/27090)
- [Go time package monotonic-clock writeup (VictoriaMetrics)](https://victoriametrics.com/blog/go-time-monotonic-wall-clock/)
- [Alpine `tzdata` missing for `TZ` env var (rabbitmq#460)](https://github.com/docker-library/rabbitmq/issues/460)
- [Timezone in Docker Alpine](https://www.grainger.xyz/posts/timezone-in-docker-alpine-not-using-environment-variable-tz)
- [Golang + CGO + Alpine SQLite build guidance](https://drailing.net/2018/05/tobi-golang-alpine-image-with-cgo-for-sqlite3/)
- [Go SQLite static build with multistage Docker](https://github.com/sanderhahn/alpine-cgo)
- [Levenshtein distance-1 false positives on short words](https://medium.com/@sohail_saifii/implementing-fuzzy-search-with-levenshtein-distance-8b5c349057de)
- [Unicode normalization NFC/NFD reference (Unicode UAX #15)](https://www.unicode.org/standard/reports/tr15/tr15-21.html)
- [Echo-loop pattern in WhatsApp self-chat handling (openclaw#55174)](https://github.com/openclaw/openclaw/issues/55174)
- [Go fsnotify debounce + concurrent-write pattern](https://dev.to/asoseil/from-chaos-to-signal-taming-high-frequency-os-events-in-go-4p8k)

---
*Pitfalls research for: whatsmeow-based single-group fuzzy-match reply bot (bot-camomila)*
*Researched: 2026-05-22*
