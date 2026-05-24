---
status: partial
phase: 03-owner-commands-operability
source: [03-VERIFICATION.md]
started: 2026-05-24T00:00:00Z
updated: 2026-05-24T00:00:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. !pause / !resume end-to-end in live group
expected: Bot sends threaded reply "paused" after !pause; subsequent trigger messages dropped with reason:kill_switch in logs. !resume replies "resumed" and restores normal matching.
result: [pending]

### 2. Unauthorized sender silently ignored
expected: No reply sent; debug log shows reason:owner_command_denied; kill switch state unchanged.
result: [pending]

### 3. Hot-reload does not reset kill switch
expected: Bot remains paused after config reload; trigger message dropped with reason:kill_switch.
result: [pending]

### 4. allow_admin_commands admin path (optional — only if config uses it)
expected: Group admin JID not in owner_jids triggers GetGroupInfo lookup, bot executes command and replies "paused".
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
