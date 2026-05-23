---
phase: 2
plan: 5
wave: 2
depends_on: [1, 2, 3, 4]
files_modified:
  - internal/pipeline/pipeline.go
  - internal/pipeline/pipeline_test.go
  - internal/pipeline/ratelimit.go
autonomous: true
user_setup: []

must_haves:
  truths:
    - "Pipeline composes kill switch → quiet hours → match → cooldown → rate cap in order"
    - "Pipeline returns a Decision with reason for every input"
    - "Rate limiter enforces per-min and per-hour caps"
    - "Answer is randomly picked and variable-substituted"
    - "Quoted text is matched with quote-chain loop prevention"
  artifacts:
    - "internal/pipeline/pipeline.go exists with Handle() method"
    - "internal/pipeline/pipeline_test.go exists with full gate-chain tests"
---

# Plan 2.5: Pipeline Orchestrator

<objective>
Build the pipeline orchestrator that composes all wave-1 packages into the full gate chain: kill switch → quiet hours → match (body + quoted) → cooldown → rate cap → pick answer → substitute variables.

Purpose: This is the central dispatch logic. Given a domain.Message and a config.Snapshot, it returns a Decision (reply / drop + reason).
Output: `internal/pipeline/` package with `Pipeline` type, `Handle()`, rate limiter, and tests.
</objective>

<context>
Load for context:
- .gsd/SPEC.md (all Phase 2 requirements)
- .gsd/DECISIONS.md (Phase 2 decisions)
- internal/matcher/matcher.go
- internal/cooldown/cooldown.go
- internal/quiethours/quiethours.go
- internal/killswitch/killswitch.go
- internal/config/config.go (Snapshot, ResolvedMatcher types)
- internal/domain/message.go
</context>

<tasks>

<task type="auto">
  <name>Implement the pipeline orchestrator and rate limiter</name>
  <files>internal/pipeline/pipeline.go, internal/pipeline/ratelimit.go</files>
  <action>
    Create package `pipeline` with:

    **ratelimit.go:**
    1. `type RateLimiter struct`:
       - mu        sync.Mutex
       - minuteLog []time.Time  // timestamps of sends in the last minute
       - hourLog   []time.Time  // timestamps of sends in the last hour
       - clock     func() time.Time

    2. `func NewRateLimiter(clock func() time.Time) *RateLimiter`

    3. `func (r *RateLimiter) Allow(perMin, perHour int) bool`:
       - Lock mutex
       - Prune minuteLog entries older than 1 min; prune hourLog entries older than 1 hour
       - If len(minuteLog) >= perMin → return false
       - If len(hourLog) >= perHour → return false
       - Return true (but do NOT record yet — Record() is separate)

    4. `func (r *RateLimiter) Record()`:
       - Append now to minuteLog and hourLog

    **pipeline.go:**
    1. `type Decision struct`:
       - Reply       bool
       - Answer      string        // the final answer text (with variables substituted)
       - MatcherName string        // which matcher fired
       - MatchedWord string        // the input token that matched
       - DropReason  string        // why it was dropped (empty if Reply == true)

    2. `type Pipeline struct`:
       - killSwitch  *killswitch.Switch
       - cooldowns   *cooldown.Tracker
       - rateLimiter *RateLimiter
       - rng         *rand.Rand     // from math/rand/v2, seeded; for answer picking

    3. `func New(ks *killswitch.Switch, cd *cooldown.Tracker, rl *RateLimiter) *Pipeline`

    4. `func (p *Pipeline) Handle(msg domain.Message, snap *config.Snapshot) Decision`:
       Gate chain (order is critical):

       a. **Kill switch gate**: if p.killSwitch.IsPaused() → return Decision{DropReason: "kill_switch"}

       b. **Quiet hours gate**: if quiethours.IsActive(now, snap.Location, snap.Limits.QuietHours.Start, snap.Limits.QuietHours.End) → return Decision{DropReason: "quiet_hours"}

       c. **Match gate**:
          - Normalize msg.Text with matcher.Normalize()
          - Try matcher.Match(normalizedText, snap.Matchers)
          - If no match on body AND msg.QuotedBody != "":
            - Skip if msg.QuotedSenderJID == msg.SenderJID (self-quote, not dangerous but not useful)
            — Actually: skip if quoted author is the bot. But the pipeline doesn't know the bot's JID.
              → Use a simple heuristic: the adapter sets QuotedSenderJID = "" when the quoted author is IsFromMe.
              So: if msg.QuotedBody != "" and msg.QuotedSenderJID != "":
                Normalize msg.QuotedBody, try matcher.Match()
          - If still no match → return Decision{DropReason: "no_match"}

       d. **Cooldown gate**: if !p.cooldowns.Allow(result.MatcherName, msg.SenderJID, matcherCooldown, snap.UserCooldownDuration) → return Decision{DropReason: "cooldown"}
          Where matcherCooldown comes from the matched ResolvedMatcher.CooldownDuration.
          (Need to look up the matched ResolvedMatcher by name from snap.Matchers to get CooldownDuration.)

       e. **Rate cap gate**: if !p.rateLimiter.Allow(snap.Limits.RateCap.PerMin, snap.Limits.RateCap.PerHour) → return Decision{DropReason: "rate_cap"}

       f. **Pick answer**: randomly select from matched ResolvedMatcher.Answers using p.rng.

       g. **Substitute variables**:
          - Replace `{MATCHED_WORD}` with result.MatchedWord
          - Replace `{REPLIED_USER}` with msg.SenderPushName (may be empty per decision)

       h. **Record**: p.cooldowns.Record(result.MatcherName, msg.SenderJID); p.rateLimiter.Record()

       i. Return Decision{Reply: true, Answer: answer, MatcherName: result.MatcherName, MatchedWord: result.MatchedWord}

    AVOID: Calling p.cooldowns.Record() or p.rateLimiter.Record() if the decision is to drop.
    AVOID: Holding the rate limiter lock while doing matching (lock only for Allow/Record).
    AVOID: Resetting cooldown state on any event other than a successful fire.
  </action>
  <verify>go build ./internal/pipeline/... succeeds</verify>
  <done>Pipeline.Handle() runs the full gate chain and returns a Decision with reason for every input</done>
</task>

<task type="auto">
  <name>Write pipeline tests</name>
  <files>internal/pipeline/pipeline_test.go</files>
  <action>
    Create table-driven tests with fake clock and test fixtures:

    Build a helper that creates a Pipeline with a known kill switch, cooldown tracker (fake clock), and rate limiter (fake clock).
    Build a test config.Snapshot with 1-2 matchers for test purposes.

    Test cases:
    1. TestHandle_KillSwitchDrops — pause kill switch → DropReason == "kill_switch"
    2. TestHandle_QuietHoursDrops — set quiet hours active → DropReason == "quiet_hours"
    3. TestHandle_NoMatchDrops — message with no trigger words → DropReason == "no_match"
    4. TestHandle_MatchFires — message with trigger word → Reply == true, Answer non-empty
    5. TestHandle_CooldownDrops — fire once, immediately send same message → DropReason == "cooldown"
    6. TestHandle_RateCapDrops — fire perMin+1 times → last one → DropReason == "rate_cap"
    7. TestHandle_QuotedTextMatch — no match on body, but quoted text has trigger → Reply == true
    8. TestHandle_QuotedSelfSkipped — quoted text with QuotedSenderJID == "" (bot's own quote) → no match on quoted
    9. TestHandle_VariableSubstitution — answer with {MATCHED_WORD} is replaced correctly
    10. TestHandle_GateOrder — kill switch checked before quiet hours before match (verify by enabling kill switch + quiet hours + match — DropReason should be "kill_switch" not "quiet_hours")

    AVOID: Mocking the matcher — use real matcher with real config.
    AVOID: Tests that only check Reply == true/false without verifying DropReason or MatcherName.
  </action>
  <verify>go test -v ./internal/pipeline/... — all tests pass</verify>
  <done>≥10 test cases covering every gate in the pipeline chain</done>
</task>

</tasks>

<verification>
After all tasks, verify:
- [ ] `go test -v ./internal/pipeline/...` — all tests pass
- [ ] `go vet ./internal/pipeline/...` — no issues
- [ ] Every gate in the chain is tested with a drop case
- [ ] Variable substitution is tested
</verification>

<success_criteria>
- [ ] All tasks verified
- [ ] Pipeline correctly composes all gates
- [ ] ≥10 test cases pass
</success_criteria>
