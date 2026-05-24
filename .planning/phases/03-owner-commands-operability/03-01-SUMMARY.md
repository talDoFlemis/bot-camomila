---
phase: 03-owner-commands-operability
plan: "01"
subsystem: config + ownercommands
tags: [config, ownercommands, killswitch, tdd]
dependency_graph:
  requires: []
  provides:
    - "AllowAdminCommands field in ListenerConfig and ResolvedListener"
    - "ownercommands.Handle() — pure-Go kill-switch dispatch"
  affects:
    - "internal/config — extended with AllowAdminCommands"
    - "internal/ownercommands — new package"
tech_stack:
  added: []
  patterns:
    - "TDD RED/GREEN for config struct extension and ownercommands package"
    - "Zero-value safe bool field (yaml omitempty not needed for bool)"
key_files:
  created:
    - internal/ownercommands/ownercommands.go
    - internal/ownercommands/ownercommands_test.go
    - internal/config/load_test.go
  modified:
    - internal/config/config.go
    - internal/config/load.go
    - config.schema.json
    - config.example.yaml
decisions:
  - "AllowAdminCommands placed after OwnerJIDs in both ListenerConfig and ResolvedListener for consistency"
  - "ownercommands package has no dependency beyond internal/killswitch — hexagonal boundary intact"
  - "No bool validation logic in validate() — zero-value false is the intended default (per D-04)"
metrics:
  duration: "~15 minutes"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 7
---

# Phase 03 Plan 01: Config AllowAdminCommands + ownercommands Package Summary

**One-liner:** AllowAdminCommands bool added to config types with zero-value safety, ownercommands.Handle() pure-Go kill-switch dispatch created with table-driven TDD.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Failing tests for AllowAdminCommands | 3453cc2 | internal/config/load_test.go |
| 1 (GREEN) | Add AllowAdminCommands to config types | 48f5098 | internal/config/config.go, load.go |
| 2 (RED) | Failing tests for ownercommands.Handle | 9aa4086 | internal/ownercommands/ownercommands_test.go |
| 2 (GREEN) | Create ownercommands package | 7ab7476 | internal/ownercommands/ownercommands.go |
| 3 | Update schema and example YAML | e69cf6f | config.schema.json, config.example.yaml |

## Verification Evidence

- `go build ./...` exits 0
- `go test ./internal/config/... ./internal/ownercommands/...` exits 0 (6 tests pass)
- `grep -c "AllowAdminCommands" internal/config/config.go` = 2 (one in ListenerConfig, one in ResolvedListener)
- `grep -c "AllowAdminCommands" internal/config/load.go` = 1
- `go vet ./...` exits 0
- No whatsmeow imports in internal/ownercommands/ (hexagonal boundary)
- config.example.yaml loads successfully through config.Load() (QR screen shown, no parse error)

## Deviations from Plan

None — plan executed exactly as written.

## TDD Gate Compliance

- Task 1: RED commit (3453cc2: test) before GREEN commit (48f5098: feat) — gate compliant
- Task 2: RED commit (9aa4086: test) before GREEN commit (7ab7476: feat) — gate compliant

## Known Stubs

None — all fields are wired through to ResolvedListener. AllowAdminCommands is not yet consumed by the adapter (that is Plan 02's responsibility by design).

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries beyond what is documented in the plan's threat model (T-03-01-01: AllowAdminCommands bool accepted, zero-value safe, no exec path widening at config layer).

## Self-Check: PASSED

- internal/ownercommands/ownercommands.go: FOUND
- internal/ownercommands/ownercommands_test.go: FOUND
- internal/config/load_test.go: FOUND
- Commits 3453cc2, 48f5098, 9aa4086, 7ab7476, e69cf6f: all present in git log
