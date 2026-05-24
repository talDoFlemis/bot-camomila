---
phase: 03-owner-commands-operability
reviewed: 2026-05-24T00:00:00Z
depth: standard
files_reviewed: 10
files_reviewed_list:
  - internal/ownercommands/ownercommands.go
  - internal/ownercommands/ownercommands_test.go
  - internal/config/load_test.go
  - internal/config/config.go
  - internal/config/load.go
  - config.schema.json
  - config.example.yaml
  - internal/whatsappadapter/client.go
  - internal/whatsappadapter/inbound.go
  - internal/app/app.go
findings:
  critical: 2
  warning: 4
  info: 2
  total: 8
status: issues_found
---

# Phase 03: Code Review Report

**Reviewed:** 2026-05-24
**Depth:** standard
**Files Reviewed:** 10
**Status:** issues_found

## Summary

This phase delivers the `ownercommands` package (`!pause` / `!resume`), the `AllowAdminCommands` config field, two-tier authorization in the adapter, and the associated config schema and tests. The `ownercommands` package itself is clean and its test suite is thorough. The config schema, hot-reload watcher, and adapter are generally well-structured. However, two correctness bugs were found in `internal/config/load.go` that can cause runtime panics or silent matcher failures, and the owner-JID comparison in the adapter has a latent authorization correctness gap due to missing normalization at load time.

Cross-referencing with CLAUDE.md invariants: the kill switch is correctly kept outside the config snapshot, `ToNonAD()` is called on the _sender_ side, and `time.AfterFunc` with 200 ms debounce is present. The gaps are in defensive validation of config values that bypass the YAML-layer minItems/enum constraints.

---

## Critical Issues

### CR-01: Empty cluster `answers` array not validated — runtime panic on first match

**File:** `internal/config/load.go:44-48`
**Also:** `internal/pipeline/pipeline.go:104`

**Issue:** `validate()` builds the cluster map with `clusterMap[ac.Name] = ac.Answers` but never checks `len(ac.Answers) > 0`. The JSON Schema declares `minItems: 1` for `answers`, but `goccy/go-yaml` with `DisallowUnknownField()` does not enforce JSON Schema numeric constraints — it only rejects unknown field names. A config file containing an empty answers array (`answers: []`) passes `Load()` without error, and `ResolvedMatcher.Answers` is stored as a zero-length slice.

At runtime, `pipeline.go` line 104 calls:

```go
answer := matchedMatcher.Answers[p.rng.IntN(len(matchedMatcher.Answers))]
```

`rand.IntN(0)` panics with `"invalid argument to IntN"`, crashing the bot on the first message that reaches a matcher backed by an empty cluster. The panic cannot be recovered by normal flow because it is buried inside the event handler.

**Fix:** Add an explicit check in `validate()` after resolving the cluster map:

```go
for _, ac := range cfg.AnswersClusters {
    if _, exists := clusterMap[ac.Name]; exists {
        return nil, fmt.Errorf("clusters name %q appears more than once", ac.Name)
    }
    if len(ac.Answers) == 0 {
        return nil, fmt.Errorf("cluster %q: answers must not be empty", ac.Name)
    }
    clusterMap[ac.Name] = ac.Answers
}
```

---

### CR-02: Negative `distance` value passes validation — matcher silently never fires

**File:** `internal/config/load.go:81-99`

**Issue:** The `distance` field is read from YAML (`distance = lv.Distance`) and the subsequent `switch distance` block only validates word-length constraints for values `1` and `2`. It does not reject negative values or values greater than `2`. The JSON Schema `enum: [0, 1, 2]` is again not enforced by the YAML decoder.

For `distance: -1`, the stored `ResolvedMatcher.Distance` is `-1`. In `matcher.go` line 97:

```go
effectiveDist := m.Distance  // = -1
if cap := maxDistanceForRuneLen(runeLen); cap < effectiveDist {
    effectiveDist = cap
}
```

`cap` is always `>= 0`, so `cap < -1` is always `false`. `effectiveDist` stays `-1`. Then `dist <= -1` is always `false` (Levenshtein distance is non-negative). The matcher produces zero matches and logs nothing — the bot runs silently broken with no error or warning.

**Fix:** Add explicit range validation in `validate()` after reading `distance`:

```go
if distance < 0 || distance > 2 {
    return nil, fmt.Errorf("matcher %q: distance %d is invalid (valid: 0, 1, 2)", m.Name, distance)
}
```

---

## Warnings

### WR-01: `OwnerJIDs` stored as raw YAML strings, not normalized to NonAD form

**File:** `internal/config/load.go:140-163`

**Issue:** `validate()` calls `types.ParseJID(raw)` to verify each owner JID is syntactically valid, but discards the parsed result and stores the raw string: `OwnerJIDs: l.OwnerJIDs`. In `inbound.go` line 165, the sender JID is correctly normalized: `senderJID := evt.Info.Sender.ToNonAD().String()`. The comparison at line 170 is then:

```go
if ownerJID == senderJID {  // ownerJID = raw config string; senderJID = NonAD form
```

If an operator configures an AD-form JID such as `"1234567890:10@s.whatsapp.net"`, `ToNonAD()` on the sender produces `"1234567890@s.whatsapp.net"`, and the comparison silently fails — the owner is denied command access with no error logged. This does not grant unauthorized access (fail-deny, not fail-open), but it is a correctness bug that is hard to diagnose.

**Fix:** Normalize at load time and store the NonAD form:

```go
parsedJID, err := types.ParseJID(raw)
if err != nil {
    return nil, fmt.Errorf("listeners[%d].owner_jids[%d] %q is invalid: %w", i, j, raw, err)
}
normalizedOwnerJIDs = append(normalizedOwnerJIDs, parsedJID.ToNonAD().String())
```

Then store `normalizedOwnerJIDs` instead of `l.OwnerJIDs` in `ResolvedListener`.

---

### WR-02: Pending debounce timer not stopped when context is cancelled

**File:** `internal/config/watcher.go:62-82`

**Issue:** When `ctx.Done()` fires in the main loop, `Run()` returns `nil` immediately. If a `time.AfterFunc` debounce timer is currently pending (fired within the preceding 200 ms), it will still fire in a background goroutine and call `w.reload()` after `Run()` has returned. During a normal shutdown this means a potentially redundant file I/O and `store.Swap()` can occur concurrently with `Disconnect()`. While `store.Swap()` is individually atomic, this represents an unexpected state mutation during the shutdown sequence.

Additionally, `debounce.Stop()` on line 80 discards the return value. If `Stop()` returns `false` (timer already expired), the old `reload()` is already running or done, and the code then immediately schedules _another_ `time.AfterFunc`. This can cause two consecutive reloads on a single burst of events when the timer fires just before `Stop()` is called.

**Fix:** Stop the debounce timer before returning on cancellation:

```go
case <-ctx.Done():
    if debounce != nil {
        debounce.Stop()
    }
    return nil
```

For the double-reload issue, check the return value of `Stop()`:

```go
if debounce != nil && !debounce.Stop() {
    // timer already fired; drain if needed (no channel to drain for AfterFunc)
    // just replace — the in-flight reload is harmless
}
debounce = time.AfterFunc(200*time.Millisecond, w.reload)
```

---

### WR-03: `logJoinedGroups` uses `context.Background()` instead of the startup context

**File:** `internal/whatsappadapter/client.go:172`

**Issue:** `logJoinedGroups()` is called during `Start()` and fetches the list of joined groups from WhatsApp. It creates a detached `context.Background()` rather than using the cancellable `ctx` already in scope:

```go
groups, err := a.client.GetJoinedGroups(context.Background())
```

If the app context is cancelled (e.g., fast shutdown during startup), this call continues running to its internal timeout rather than being cancelled promptly.

**Fix:** Pass the startup context through to `logJoinedGroups`:

```go
func (a *Adapter) logJoinedGroups(ctx context.Context) {
    groups, err := a.client.GetJoinedGroups(ctx)
    ...
}
// caller:
a.logJoinedGroups(ctx)
```

---

### WR-04: `sendReply` and `sendCommandAck` goroutines use `context.Background()` — not cancelled on shutdown

**File:** `internal/whatsappadapter/inbound.go:242`, `inbound.go:282`

**Issue:** Both `sendCommandAck` and `sendReply` create a fresh `context.WithTimeout(context.Background(), 30*time.Second)`. These goroutines are fire-and-forget (`go a.sendReply(...)`, `go a.sendCommandAck(...)`). When the app shuts down (ctx cancelled → `adapter.Disconnect()` called), in-flight send goroutines are not cancelled. They continue running for up to 30 seconds (or until the `SendMessage` call returns, which may fail after `Disconnect`). In the `sendReply` case there is additionally a 2–8 second `time.Sleep` before the context is even created, extending the window further.

This is not a data corruption issue, but it means shutdown is not prompt and goroutines continue running after `Disconnect()` is called on the underlying client, which may produce spurious `send_error` log entries.

**Fix:** Store the app-level context in `Adapter` (set during `Start`) and use it as the parent for per-send timeouts:

```go
// In sendReply / sendCommandAck:
ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
```

Where `a.ctx` is the `ctx` from `Start()` (the one already associated with `a.cancel`).

---

## Info

### IN-01: Misleading error message — `err=%v` with literal `nil` in integrity check failure path

**File:** `internal/whatsappadapter/client.go:92`

**Issue:** The error format string on line 92 includes `err=%v` but passes the literal `nil` as the argument:

```go
return fmt.Errorf("SQLite integrity check failed: result=%q err=%v", integrityResult, nil)
```

This produces a message like `SQLite integrity check failed: result="corruption detail" err=<nil>`, which is confusing — the reader might think no error occurred. The `err=%v` parameter is meaningless here since the failure path is the non-"ok" result branch (no Go error was returned by `Scan`; the integrity failure is in the string value).

**Fix:**

```go
return fmt.Errorf("SQLite integrity check failed: result=%q", integrityResult)
```

---

### IN-02: `distance > 2` silently passes config load and is silently capped by matcher

**File:** `internal/config/load.go:81-99`, `internal/matcher/matcher.go:97-98`

**Issue:** A configured `distance: 3` passes `Load()` without error. At match time, `maxDistanceForRuneLen()` caps the effective distance to 2 for long tokens, 1 for medium tokens, and 0 for short tokens. The operator's configured value of `3` is silently ignored. While the matcher behavior is functionally bounded (it doesn't produce worse-than-expected matches), the config is semantically incorrect and the operator receives no feedback. This is distinct from the distance=-1 BLOCKER: distance>2 works but misrepresents operator intent.

The CR-02 fix above (explicit range check at load time) also resolves this info item.

---

_Reviewed: 2026-05-24_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
