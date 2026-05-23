---
phase: 2
plan: 3
wave: 1
depends_on: []
files_modified:
  - internal/cooldown/cooldown.go
  - internal/cooldown/cooldown_test.go
autonomous: true
user_setup: []

must_haves:
  truths:
    - "Cooldown uses time.Since (monotonic-safe) for duration checks"
    - "Per-matcher and per-user-per-matcher cooldowns are independent gates"
    - "Both gates must pass for a match to fire"
    - "Reaper goroutine periodically cleans expired entries"
    - "Clock is injectable for testing"
  artifacts:
    - "internal/cooldown/cooldown.go exists"
    - "internal/cooldown/cooldown_test.go exists with timing tests"
---

# Plan 2.3: Cooldown Engine

<objective>
Build the in-memory cooldown engine as a standalone pure-Go package. Manages per-matcher and per-user-per-matcher cooldown windows with a background reaper.

Purpose: Prevents the bot from spamming replies. Both the per-matcher cooldown and per-user cooldown must pass before a match can fire.
Output: `internal/cooldown/` package with `Tracker` type and tests.
</objective>

<context>
Load for context:
- .gsd/SPEC.md (COOLDOWN-01 through COOLDOWN-04)
- .gsd/research/PITFALLS.md (Pitfall 7: Cooldown drift on long uptimes)
</context>

<tasks>

<task type="auto">
  <name>Implement the cooldown tracker</name>
  <files>internal/cooldown/cooldown.go</files>
  <action>
    Create package `cooldown` with:

    1. `type Clock func() time.Time` — injectable clock for testing. Default: `time.Now`.

    2. `type Tracker struct`:
       - perMatcher sync.Map  // map[string]time.Time — keyed by matcher name
       - perUser    sync.Map  // map[string]time.Time — keyed by "matcherName:senderJID"
       - clock      Clock

    3. `func NewTracker(clock Clock) *Tracker`:
       - If clock is nil, use time.Now
       - Returns initialized Tracker

    4. `func (t *Tracker) Allow(matcherName, senderJID string, matcherCooldown, userCooldown time.Duration) bool`:
       - Check per-matcher cooldown first (cheaper — single key lookup):
         Load lastFire from perMatcher[matcherName]
         If exists and time.Since(lastFire) < matcherCooldown → return false
       - Check per-user cooldown second:
         Load lastFire from perUser["matcherName:senderJID"]
         If exists and t.clock().Sub(lastFire) < userCooldown → return false
       - Both passed → return true
       - NOTE: Do NOT record the fire time here — the caller records after successfully sending.
         Use t.clock() consistently, NOT time.Now() directly.

    5. `func (t *Tracker) Record(matcherName, senderJID string)`:
       - Store t.clock() in perMatcher[matcherName]
       - Store t.clock() in perUser["matcherName:senderJID"]

    6. `func (t *Tracker) StartReaper(ctx context.Context, interval time.Duration)`:
       - Run a goroutine with a ticker at `interval`
       - On each tick, range over perMatcher and perUser
       - Delete entries where t.clock().Sub(value) > 1 hour (expired + generous buffer)
       - Stop when ctx is cancelled

    AVOID: Using time.Now() directly in Allow/Record — always use t.clock() for testability.
    AVOID: Using time.Unix() round-trips — preserve monotonic clock readings (Pitfall 7).
    AVOID: Recording fire time inside Allow() — the caller must call Record() explicitly after
    successful send to avoid recording fires that were dropped by rate cap or quiet hours.
  </action>
  <verify>go build ./internal/cooldown/... succeeds</verify>
  <done>Tracker with Allow(), Record(), StartReaper() using injectable clock and sync.Map</done>
</task>

<task type="auto">
  <name>Write cooldown tests</name>
  <files>internal/cooldown/cooldown_test.go</files>
  <action>
    Create tests using testify/assert with a fake clock:

    1. TestAllow_NoCooldownRecorded:
       - Fresh tracker → Allow() returns true

    2. TestAllow_MatcherCooldownBlocks:
       - Record a fire → immediately call Allow() with 5-min matcher cooldown → returns false
       - Advance fake clock past cooldown → Allow() returns true

    3. TestAllow_UserCooldownBlocks:
       - Record a fire → Allow() with same user returns false
       - Allow() with different user returns true (user cooldown is per-user)

    4. TestAllow_BothMustPass:
       - Matcher cooldown expired but user cooldown still active → false
       - User cooldown expired but matcher cooldown still active → false
       - Both expired → true

    5. TestRecord_SetsTimestamps:
       - Record() then check Allow() blocks for the correct durations

    6. TestReaper_CleansExpiredEntries:
       - Record a fire → advance clock > 1 hour → trigger reaper
       - Verify entries are cleaned (Allow with same params returns true)

    Implement the fake clock as:
      var now time.Time
      fakeClock := func() time.Time { return now }
    Advance by mutating `now`.

    AVOID: Using time.Sleep in tests — use fake clock only.
    AVOID: Testing only the happy path — include both "blocked" and "allowed" scenarios.
  </action>
  <verify>go test -v ./internal/cooldown/... — all tests pass</verify>
  <done>≥6 test cases covering fresh, blocked, expired, per-user isolation, reaper cleanup</done>
</task>

</tasks>

<verification>
After all tasks, verify:
- [ ] `go test -v ./internal/cooldown/...` — all tests pass
- [ ] `go vet ./internal/cooldown/...` — no issues
- [ ] Monotonic clock usage verified (no time.Unix round-trips)
</verification>

<success_criteria>
- [ ] All tasks verified
- [ ] Cooldown correctly blocks and expires with injectable clock
- [ ] ≥6 test cases pass
</success_criteria>
