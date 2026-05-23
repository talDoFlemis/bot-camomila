# Phase 1: Session & Config Foundations - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-23
**Phase:** 1-Session & Config Foundations
**Areas discussed:** Config YAML schema, CLI / env interface

---

## Config YAML Schema

### Which config schema should the structs use?

| Option | Description | Selected |
|--------|-------------|----------|
| Clusters | Top-level answers_cluster pools; matchers reference by name. Resolver expands cluster → answers slice at load time. | ✓ |
| Flat answers per matcher | Each matcher has inline answers: [...]. No cluster concept. Simpler but requires duplication for shared answers. | |
| Both (optional) | Matcher accepts either cluster: name OR inline answers: [...]. Config validator requires exactly one. | |

**User's choice:** Clusters (as shown in config.example.yaml)

---

### What quiet-hours and global-rate-cap config lives at the top level?

| Option | Description | Selected |
|--------|-------------|----------|
| All in one top-level block | Single flat top-level keys: group_jid, owner_jids, quiet_hours, rate_cap, db_path. | |
| Nested under sections | Group under keys: scope:, limits:, log:, db:. More structured. | ✓ |

**User's choice:** Nested under sections

---

### What section names and keys?

| Option | Description | Selected |
|--------|-------------|----------|
| scope / limits / log | scope: { group_jid, owner_jids }; limits: { quiet_hours, rate_cap }; log: { format } | ✓ |
| bot / behavior / deploy | bot: { group_jid, owner_jids }; behavior: { quiet_hours, rate_cap }; deploy: { db_path } | |
| Let me type the shape | User describes custom structure | |

**User's choice:** scope / limits / log (plus db: for SQLite path)

---

## CLI / Env Interface

### How should the config path be supplied?

| Option | Description | Selected |
|--------|-------------|----------|
| --config flag + BOT_CONFIG env | Flag wins over env. Clean precedence. | ✓ |
| Env var only | No CLI flags. Simpler binary but less ergonomic for local dev. | |
| --config flag only | No env var. Docker compose uses CMD override. | |

**User's choice:** --config flag + BOT_CONFIG env var

---

### Where does the SQLite session DB path live?

| Option | Description | Selected |
|--------|-------------|----------|
| Inside the YAML (db: { path }) | Everything configured in one place. No extra flag. | ✓ |
| --db flag + BOT_DB_PATH env | Separate CLI flag mirrors --config. More Docker-flexible but doubles interface. | |
| Hardcoded default only | Always ./session.sqlite. Simplest but inflexible. | |

**User's choice:** Inside the YAML under db: { path }

---

## Claude's Discretion

- **Package structure:** `cmd/bot/main.go` + `internal/` from day 1 (hexagonal architecture as designed in research). Not discussed — user skipped this area.
- **Log format selection:** TTY auto-detect via isatty. Text in terminal, JSON otherwise. Not discussed — user skipped this area.

## Deferred Ideas

None — discussion stayed within Phase 1 scope.
