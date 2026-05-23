---
phase: 2
plan: 1
wave: 1
depends_on: []
files_modified:
  - go.mod
  - go.sum
  - internal/domain/message.go
  - internal/config/config.go
  - internal/config/load.go
autonomous: true
user_setup: []

must_haves:
  truths:
    - "agnivade/levenshtein and stretchr/testify are in go.mod"
    - "domain.Message has QuotedBody, QuotedSenderJID, SenderPushName fields"
    - "config.MatcherConfig has CooldownSec field"
    - "config.ResolvedMatcher has CooldownDuration field"
    - "config.Snapshot has a resolved *time.Location for quiet hours"
  artifacts:
    - "go.mod includes agnivade/levenshtein and stretchr/testify"
    - "domain.Message type enriched with Phase 2 fields"
---

# Plan 2.1: Dependencies & Domain/Config Type Extensions

<objective>
Add the new Go dependencies needed for Phase 2 (Levenshtein matching, test assertions) and extend the domain.Message and config types with the fields the matcher pipeline, cooldown, and reply subsystems require.

Purpose: Establish the shared types and dependencies that all other Phase 2 packages will import.
Output: Updated go.mod, enriched domain.Message, extended config types with cooldown/rate fields.
</objective>

<context>
Load for context:
- .gsd/SPEC.md
- .gsd/ARCHITECTURE.md
- internal/domain/message.go
- internal/config/config.go
- internal/config/load.go
</context>

<tasks>

<task type="auto">
  <name>Add Phase 2 Go dependencies</name>
  <files>go.mod, go.sum</files>
  <action>
    Run:
      go get github.com/agnivade/levenshtein@latest
      go get github.com/stretchr/testify@latest
      go mod tidy

    AVOID: Upgrading existing dependencies (whatsmeow is pinned to a specific pseudo-version).
    Use `go get` for the new packages only.
  </action>
  <verify>go build ./... succeeds; go.mod contains both new packages</verify>
  <done>go.mod lists agnivade/levenshtein and stretchr/testify; go build ./... passes</done>
</task>

<task type="auto">
  <name>Extend domain.Message with Phase 2 fields</name>
  <files>internal/domain/message.go</files>
  <action>
    Add these fields to the Message struct:

    - QuotedBody string        // plain text of the quoted message (empty if no quote)
    - QuotedSenderJID string   // JID of the quoted message's original sender (empty if no quote)
    - SenderPushName string    // display name of the sender (may be empty)

    Keep the existing fields unchanged. No imports needed beyond what's already there.

    AVOID: Adding whatsmeow types here — this is a transport-agnostic domain type.
    AVOID: Removing or reordering existing fields — other code depends on the current layout.
  </action>
  <verify>go build ./internal/domain/... succeeds</verify>
  <done>domain.Message has QuotedBody, QuotedSenderJID, SenderPushName fields with doc comments</done>
</task>

<task type="auto">
  <name>Extend config types with cooldown duration and resolved location</name>
  <files>internal/config/config.go, internal/config/load.go</files>
  <action>
    In config.go:
    1. Add `CooldownSec int `yaml:"cooldown_sec"`` field to MatcherConfig (per-matcher cooldown in seconds, default 300 = 5 min)
    2. Add `UserCooldownSec int `yaml:"user_cooldown_sec"`` field to LimitsConfig (global per-user cooldown in seconds, default 900 = 15 min)
    3. Add `CooldownDuration time.Duration` field to ResolvedMatcher (resolved from CooldownSec at load time)
    4. Add `Location *time.Location` field to Snapshot (resolved from QuietHours.Timezone at load time)
    5. Add `UserCooldownDuration time.Duration` field to Snapshot (resolved from LimitsConfig.UserCooldownSec)

    In load.go (validate function):
    1. After CHECK 3 (timezone validation), store the resolved `*time.Location` in a variable
    2. For each matcher, resolve CooldownSec → CooldownDuration (time.Duration). Use 300s (5 min) default if CooldownSec is 0.
    3. Resolve UserCooldownSec → UserCooldownDuration. Use 900s (15 min) default if 0.
    4. Set Snapshot.Location to the resolved location (nil is acceptable if no quiet hours configured)

    AVOID: Changing the validation order or removing any existing checks.
    AVOID: Using time.Local — always use the explicitly resolved location.
  </action>
  <verify>go build ./... succeeds; existing tests (if any) pass</verify>
  <done>Config types have cooldown and location fields; validate() resolves them from raw YAML values</done>
</task>

</tasks>

<verification>
After all tasks, verify:
- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] go.mod contains agnivade/levenshtein and stretchr/testify
- [ ] domain.Message has 8 fields (5 original + 3 new)
- [ ] config.ResolvedMatcher has CooldownDuration field
- [ ] config.Snapshot has Location and UserCooldownDuration fields
</verification>

<success_criteria>
- [ ] All tasks verified
- [ ] No compile errors
- [ ] Existing behavior unchanged (gates, config loading, validation)
</success_criteria>
