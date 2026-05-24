---
phase: 03-owner-commands-operability
plan: "02"
subsystem: whatsappadapter + app
tags: [whatsappadapter, killswitch, ownercommands, auth, command-dispatch]
dependency_graph:
  requires:
    - "03-01: ownercommands.Handle() + AllowAdminCommands in config"
  provides:
    - "Adapter.ks field: kill switch wired into WhatsApp adapter"
    - "handleOwnerCommand(): two-tier auth (owner JID + optional admin lookup)"
    - "sendCommandAck(): threaded reply for owner commands without jitter"
    - "Command short-circuit in handleMessage() before pipeline.Handle()"
  affects:
    - "internal/whatsappadapter — command dispatch path added"
    - "internal/app — same ks instance passed to both pipeline and adapter"
tech_stack:
  added: []
  patterns:
    - "Two-tier auth: direct JID match, then optional GetGroupInfo admin check (fail-closed)"
    - "Command short-circuit before pipeline: !pause / !resume handled even when kill switch active"
    - "Goroutine sendCommandAck: immediate reply, no jitter (unlike sendReply)"
key_files:
  created: []
  modified:
    - internal/whatsappadapter/client.go
    - internal/whatsappadapter/inbound.go
    - internal/app/app.go
decisions:
  - "snap parameter passed to handleOwnerCommand for API consistency even though only listener fields are used in current implementation"
  - "LID comparison in admin check uses p.LID.IsEmpty() guard before ToNonAD() to avoid comparing empty JIDs (RESEARCH.md Open Question 1)"
  - "No jitter in sendCommandAck (D-11): command acks are operator-facing and should be immediate"
metrics:
  duration: "~3 minutes"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 3
---

# Phase 03 Plan 02: Kill Switch Wiring & Owner Command Dispatch Summary

**One-liner:** Kill switch wired into whatsappadapter via 3-arg New(), command short-circuit in handleMessage(), and two-tier auth handleOwnerCommand() with goroutine sendCommandAck() completing the owner command end-to-end flow.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add ks field to Adapter struct and extend New() signature | e7b4487 | internal/whatsappadapter/client.go |
| 2 | Add command short-circuit and owner command handler to inbound.go | dc066f7 | internal/whatsappadapter/inbound.go |
| 3 | Update app.go to pass ks to whatsappadapter.New() | c2bfeca | internal/app/app.go |

## Verification Evidence

- `go build ./...` exits 0
- `go test ./...` exits 0 (all 8 packages with tests pass)
- `go vet ./...` exits 0
- `grep "ks *killswitch.Switch" internal/whatsappadapter/client.go` matches struct field (line 37) + New() signature
- `grep "func New(" internal/whatsappadapter/client.go` shows 3-arg signature `(cfg, pipe, ks)`
- `grep 'whatsappadapter.New(cfgStore, pipe, ks)' internal/app/app.go` = 1 match
- `grep 'normalized == "!pause"' internal/whatsappadapter/inbound.go` at line 112, before pipeline.Handle() at line 132
- `grep "handleOwnerCommand" internal/whatsappadapter/inbound.go` = 3 matches (comment + definition + call)
- `grep "sendCommandAck" internal/whatsappadapter/inbound.go` = 4 matches (comment + definition + goroutine call + log)
- `grep "owner_command_denied" internal/whatsappadapter/inbound.go` = 1 match (debug log for unauthorized)
- `grep '"owner_command"' internal/whatsappadapter/inbound.go` = 1 match (info log for authorized)
- No go.mau.fi imports in internal/ownercommands/ (hexagonal boundary intact)
- `grep 'whatsappadapter.New(cfgStore, pipe)' internal/app/app.go` = 0 matches (old 2-arg call gone)

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — all dispatch paths are fully wired. handleOwnerCommand() calls ownercommands.Handle() and launches sendCommandAck() for authorized commands.

## Threat Flags

No new threat surfaces beyond those documented in the plan's threat model:

- T-03-02-01 (mitigated): `evt.Info.Sender.ToNonAD().String()` used for senderJID — AD-suffix bypass prevented.
- T-03-02-02 (mitigated): GetGroupInfo error path is fail-closed — unauthorized on error. Both `p.JID.ToNonAD()` and `p.LID.ToNonAD()` compared when LID is non-empty.
- T-03-02-04 (mitigated): `ks` passed once to New(); config.Watcher has no reference to it — hot-reload cannot reset kill switch state.
- T-03-02-05 (mitigated): Authorized commands logged at Info with sender_jid + cmd + ack. Denied attempts logged at Debug with reason owner_command_denied.

## Self-Check: PASSED

- internal/whatsappadapter/client.go: FOUND (ks field line 37, New() 3-arg line 47)
- internal/whatsappadapter/inbound.go: FOUND (short-circuit line 112, handleOwnerCommand definition, sendCommandAck definition)
- internal/app/app.go: FOUND (whatsappadapter.New(cfgStore, pipe, ks) line 68)
- Commits e7b4487, dc066f7, c2bfeca: all present in git log
