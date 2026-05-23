---
phase: 2
plan: 4
wave: 1
depends_on: []
files_modified:
  - internal/quiethours/quiethours.go
  - internal/quiethours/quiethours_test.go
  - internal/killswitch/killswitch.go
  - internal/killswitch/killswitch_test.go
autonomous: true
user_setup: []

must_haves:
  truths:
    - "Quiet hours check uses explicit *time.Location, never time.Local"
    - "Midnight wrap-around (start > end) is correctly handled"
    - "Kill switch is atomic.Bool with Pause/Resume/IsActive methods"
    - "Kill switch state is completely independent of config reload"
  artifacts:
    - "internal/quiethours/ package exists with Check() and tests"
    - "internal/killswitch/ package exists with Switch type and tests"
---

# Plan 2.4: Quiet Hours & Kill Switch

<objective>
Build the quiet-hours checker and kill-switch as standalone pure-Go packages. Both are simple gates in the pipeline.

Purpose: Quiet hours ensures the bot stays silent during configured windows. Kill switch provides the atomic.Bool gate that Phase 3 will wire to owner DM commands.
Output: `internal/quiethours/` and `internal/killswitch/` packages with tests.
</objective>

<context>
Load for context:
- .gsd/SPEC.md (QUIET-01 through QUIET-03, OWNER-03/OWNER-06)
- .gsd/research/PITFALLS.md (Pitfall 8: Container timezone, Pitfall 12: Kill switch ignored)
- .gsd/DECISIONS.md (ADR-003: Kill Switch Outside Config Snapshot)
</context>

<tasks>

<task type="auto">
  <name>Implement quiet hours checker</name>
  <files>internal/quiethours/quiethours.go, internal/quiethours/quiethours_test.go</files>
  <action>
    Create package `quiethours` with:

    1. `func IsActive(now time.Time, loc *time.Location, start, end string) bool`:
       - If loc is nil → return false (no quiet hours configured)
       - If start == "" or end == "" → return false
       - Parse start/end as "15:04" using time.Parse
       - Convert `now` to the target location: `now = now.In(loc)`
       - Build today's start and end times in the target TZ
       - Handle midnight wrap-around (QUIET-03):
         If start > end (e.g., 22:00-08:00), the window spans midnight.
         Check: now >= start || now < end
         If start <= end (e.g., 08:00-12:00), normal window.
         Check: now >= start && now < end
       - Return true if now falls inside the quiet window

    Tests (in quiethours_test.go):
    1. TestIsActive_NilLocation → false
    2. TestIsActive_EmptyTimes → false
    3. TestIsActive_DuringQuietHours → true (e.g., 23:00 in 22:00-08:00 window)
    4. TestIsActive_OutsideQuietHours → false (e.g., 12:00 in 22:00-08:00 window)
    5. TestIsActive_MidnightWrapAround → true (e.g., 03:00 in 22:00-08:00 window)
    6. TestIsActive_NormalWindow → true/false (e.g., 10:00 in 08:00-12:00 window)
    7. TestIsActive_ExactBoundary → start inclusive, end exclusive

    AVOID: Using time.Local — always use the passed *time.Location.
    AVOID: Parsing timezone inside this function — it receives a pre-resolved *time.Location.
  </action>
  <verify>go test -v ./internal/quiethours/... — all tests pass</verify>
  <done>IsActive correctly handles normal and wrap-around windows with ≥7 test cases</done>
</task>

<task type="auto">
  <name>Implement kill switch</name>
  <files>internal/killswitch/killswitch.go, internal/killswitch/killswitch_test.go</files>
  <action>
    Create package `killswitch` with:

    1. `type Switch struct`:
       - active atomic.Bool

    2. `func New() *Switch`:
       - Returns a Switch with active=false (bot starts unpaused)

    3. `func (s *Switch) Pause()` — sets active to true
    4. `func (s *Switch) Resume()` — sets active to false
    5. `func (s *Switch) IsPaused() bool` — returns current state

    Tests (in killswitch_test.go):
    1. TestNew_StartsUnpaused → IsPaused() == false
    2. TestPause → IsPaused() == true
    3. TestResume → Pause then Resume → IsPaused() == false
    4. TestConcurrentAccess → goroutine safety (spawn 100 goroutines toggling)

    AVOID: Adding any config dependency — kill switch is completely independent (ADR-003).
    AVOID: Adding methods for commands here — Phase 3 adds the DM command parser.
  </action>
  <verify>go test -v ./internal/killswitch/... — all tests pass; go test -race ./internal/killswitch/...</verify>
  <done>Switch type with atomic.Bool; 4 tests pass including race detector</done>
</task>

</tasks>

<verification>
After all tasks, verify:
- [ ] `go test -v ./internal/quiethours/...` — all tests pass
- [ ] `go test -v ./internal/killswitch/...` — all tests pass
- [ ] `go test -race ./internal/killswitch/...` — passes
- [ ] Midnight wrap-around test explicitly covered
</verification>

<success_criteria>
- [ ] All tasks verified
- [ ] Quiet hours handles wrap-around correctly
- [ ] Kill switch is race-safe
- [ ] ≥11 test cases total across both packages
</success_criteria>
