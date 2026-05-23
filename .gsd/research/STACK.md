# Stack Research

**Domain:** Single-binary Go WhatsApp bot (whatsmeow + SQLite session store + YAML config + Docker on a VPS)
**Researched:** 2026-05-22
**Confidence:** HIGH

## TL;DR — The Stack

- **Go 1.26.3** (already set; matches whatsmeow's `toolchain` directive)
- **whatsmeow** pinned to pseudo-version `v0.0.0-20260516102357-8d3700152a69` (no semver tags exist; pin in `go.mod`)
- **modernc.org/sqlite v1.50.1** — pure-Go, CGO-free, drops cleanly into a static binary on `distroless/static-debian13`
- **goccy/go-yaml v1.19.2** for config (yaml.v3 frozen since 2022; goccy is actively maintained, strict-mode friendly)
- **agnivade/levenshtein v1.2.1** — single function, rune-safe, ~330 ns/op
- **fsnotify v1.10.1**, watching the **parent directory** (not the file) to survive editor atomic renames
- **stretchr/testify v1.11.1** on top of stdlib `testing`
- **Distroless `gcr.io/distroless/static-debian13:nonroot`** as the runtime base, multi-stage build from `golang:1.26.3-alpine`

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|---|---|---|---|
| Go (toolchain) | 1.26.3 | Language/runtime | Already chosen; whatsmeow's own `go.mod` declares `go 1.25.0` + `toolchain go1.26.3`, so 1.26.3 is the matched target. Stdlib `log/slog`, `signal.NotifyContext`, and `slices` are all native. |
| `go.mau.fi/whatsmeow` | `v0.0.0-20260516102357-8d3700152a69` (latest main as of 2026-05-16) | WhatsApp Multi-Device client | Canonical Go implementation; no Node/Baileys subprocess; no Cloud-API business verification; single static binary. Has no semver release — **pin the exact pseudo-version** and bump deliberately. |
| `go.mau.fi/whatsmeow/store/sqlstore` | (sub-package, same module) | Device-session persistence | Required by whatsmeow; speaks `database/sql` with dialects `"sqlite3"` or `"postgres"`. Schema is auto-upgraded via `container.Upgrade(ctx)`. |
| `modernc.org/sqlite` | `v1.50.1` (May 2026; SQLite 3.53.1 inside) | SQLite driver for sqlstore | **CGO-free pure Go** — keeps the binary fully static, builds on `golang:alpine` without `gcc`/`musl-dev`, and runs on `distroless/static`. Registered driver name is `"sqlite"`, **but sqlstore expects the dialect string `"sqlite3"`** — register an alias (see "whatsmeow Gotchas" below). |
| `gopkg.in/yaml.v3` *(stdlib-style)* OR **`github.com/goccy/go-yaml`** v1.19.2 | latest | Config parsing | **Pick goccy/go-yaml v1.19.2.** yaml.v3 has been frozen since `v3.0.1` (May 2022) and the upstream repo is unmaintained; goccy ships releases monthly, passes ~60 more YAML test-suite cases, supports `OmitZero`, `Strict()` (errors on unknown keys — important for a typo'd `cluster:` reference), and has no transitive deps. |
| `github.com/agnivade/levenshtein` | `v1.2.1` (Dec 2024) | Fuzzy matcher | One function (`ComputeDistance(a, b string) int`), rune-safe (works with the Portuguese accents in `config.example.yaml`), 1 allocation per call (~330 ns ASCII), benchmarked fastest of the popular forks. |
| `github.com/fsnotify/fsnotify` | `v1.10.1` (May 2026) | Hot-reload of `config.yaml` | De-facto standard. **Watch the parent directory, not the file** — editors and `kubectl cp`/`docker cp` do atomic rename-into-place, which invalidates a file-level watch (documented caveat). |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---|---|---|---|
| `log/slog` (stdlib) | Go 1.26.3 | Structured logging | Already adopted in `main.go`. Use `slog.New(slog.NewJSONHandler(os.Stdout, …))` in container, `slog.NewTextHandler` for local dev. |
| `github.com/stretchr/testify` | `v1.11.1` (Aug 2025) | Assertions + mocks | `require` for fatal table-driven assertions, `assert` for soft checks. Skip `suite` (no parallel-test support, known issue #934). |
| `go.mau.fi/whatsmeow/util/log` (`waLog`) | bundled | Logger adapter | whatsmeow expects `waLog.Logger` (it doesn't take `*slog.Logger` directly). Wrap slog by implementing `waLog.Logger` (3 methods: `Debugf/Infof/Warnf/Errorf` + `Sub`). Trivial 30-line adapter. |
| `github.com/google/uuid` | already a transitive dep of whatsmeow | UUIDs if needed | Free — no extra `require`. |

### Development Tools

| Tool | Purpose | Notes |
|---|---|---|
| `go test ./...` + `-race -shuffle=on` | Test runner | Stdlib only; use `t.Run` table-driven tests + testify `require`. |
| `golangci-lint` v1.62+ | Linter | Enable `errcheck`, `govet`, `staticcheck`, `gosec`, `revive`, `gocritic`. Run in CI. |
| `go vet` + `gofmt -s` | Formatting/vet | Stdlib; both already covered by `golangci-lint`. |
| GitHub Actions (`actions/setup-go@v5`) | CI | Matrix: `{go: ['1.26.x'], os: ['ubuntu-latest']}`. Cache modules via `cache: true`. |
| `docker buildx` | Multi-arch image build | Build `linux/amd64` (and optionally `linux/arm64` for cheap ARM VPSes). With modernc, no QEMU/cross-CGO pain. |

## Installation

```bash
# Core
go get go.mau.fi/whatsmeow@latest
go get modernc.org/sqlite@v1.50.1
go get github.com/goccy/go-yaml@v1.19.2
go get github.com/agnivade/levenshtein@v1.2.1
go get github.com/fsnotify/fsnotify@v1.10.1

# Dev
go get -t github.com/stretchr/testify@v1.11.1
```

After `go get`, run `go mod tidy` and commit `go.sum`.

## Docker Build Path (CGO-Free)

This is the **only** combination that yields a small, static, distroless image without `gcc`:

```dockerfile
# ---- build stage ----
FROM golang:1.26.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0 is the whole point — modernc.org/sqlite needs zero C
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/bot-camomila .

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=build /out/bot-camomila /app/bot-camomila
# Volumes mounted by host: /data (SQLite) and /config (config.yaml)
USER nonroot:nonroot
ENTRYPOINT ["/app/bot-camomila"]
```

Expected final image size: ~15–20 MB (binary) + ~2 MB (distroless static).

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| `modernc.org/sqlite` | `github.com/mattn/go-sqlite3` v1.14.44 | Only if you need SQLite extensions, custom C functions via `ConnectHook`, or absolute peak performance. Cost: CGO + `gcc` in build, can't use `distroless/static` (need `distroless/base` + glibc), bigger image, painful cross-arch. **Not worth it for this project.** |
| `goccy/go-yaml` v1.19.2 | `gopkg.in/yaml.v3` v3.0.1 | If you want zero new deps and absolute API stability and don't care that the upstream has shipped nothing since May 2022. Still works fine; just less actively maintained. |
| `agnivade/levenshtein` | `hbollon/go-edlib` v1.7.0 (Aug 2025) | If you ever want Damerau-Levenshtein, Jaro-Winkler, n-grams, or built-in `FuzzySearch` over a slice. For one-off `int` distance against ≤10 keywords, it's overkill. |
| `distroless/static-debian13` | `alpine:3.21` | Only if you need a shell in the container for one-off debugging. Costs you `apk`-based attack surface; you can `docker run --rm -it --entrypoint=sh` against the build stage instead. |
| `distroless/static-debian13` | `scratch` | If you want the absolute minimum and don't need CA certs or `/tmp`. Static distroless gives you `ca-certificates` and `tzdata` for free — both useful (TLS to WhatsApp servers; quiet-hours time zone). |
| `stretchr/testify` | `github.com/google/go-cmp` + stdlib | If you only need deep-equality for one or two tests. Testify's `require` is much terser for the table-driven matcher/cooldown tests you'll write. |
| `stretchr/testify` | `gotest.tools/v3` | Equivalent ergonomics, smaller community. Testify wins on ecosystem familiarity. |
| `fsnotify` | polling with `time.Ticker` + `os.Stat` mtime | Acceptable fallback on filesystems where inotify is unreliable (some FUSE/NFS mounts). Not needed on a normal VPS volume. |

## What NOT to Use

| Avoid | Why | Use Instead |
|---|---|---|
| `mattn/go-sqlite3` in a CGO-free build | Requires `CGO_ENABLED=1` + `gcc` + libc at runtime → can't use `distroless/static`, build is slower, cross-compile is painful. The page literally says "you are required to set `CGO_ENABLED=1` and have a gcc compiler in your path." | `modernc.org/sqlite v1.50.1` |
| `gopkg.in/yaml.v2` | EOL/superseded; lacks `yaml.v3`'s decoder fidelity. | `goccy/go-yaml v1.19.2` |
| `spf13/viper` for config | Heavy (pulls cobra, hcl, json, toml, ini, etcd, consul, fsnotify wrappers); opinionated about env-var bindings; overkill for one YAML file. | Hand-rolled: `goccy/go-yaml` + `fsnotify` (~80 LOC total) |
| `logrus` / `zap` (new code) | `log/slog` is now stdlib (Go 1.21+) and is what the rest of the ecosystem is migrating *to*. whatsmeow's own `waLog` interface is trivial to bridge. | `log/slog` |
| `alpine` runtime base | Ships `musl` and a full shell — both unnecessary and additional CVE surface. ~5 MB vs ~2 MB for distroless static. | `gcr.io/distroless/static-debian13:nonroot` |
| `scratch` runtime base (for *this* project) | No CA certs → TLS handshake to WhatsApp servers will fail unless you `COPY` them in yourself. No `/etc/passwd` → no `nonroot` user. | `gcr.io/distroless/static-debian13:nonroot` (gives you both for free) |
| Untagged `latest` whatsmeow in `go.mod` | API can change between commits; pseudo-version pinning is mandatory because there are no semver tags. | Pin `v0.0.0-20260516102357-8d3700152a69` and bump on a schedule with a smoke test. |

## whatsmeow Gotchas — Surface These in the Roadmap

These are non-obvious and will bite if not planned for explicitly:

1. **No semver tags.** Module is permanently in `v0.0.0-<ts>-<sha>` pseudo-version land. Pin exactly, document the pinned version in `README.md`, and treat bumps as their own PR with a manual QR-pair smoke test.
2. **Dialect string is `"sqlite3"` even when the driver is `modernc`'s `"sqlite"`.** sqlstore passes the dialect through to its own SQL-string switch (it doesn't call `sql.Open`'s driver name directly when you use `NewWithDB`). Two safe patterns:
   - Use `sqlstore.NewWithDB(db, "sqlite3", waLogger)` after `sql.Open("sqlite", "file:…")`.
   - Or, register an alias once at init: `sql.Register("sqlite3", &sqlite.Driver{})` so the high-level `sqlstore.New(ctx, "sqlite3", "file:…?_foreign_keys=on", waLogger)` form works too.
   The first form (`NewWithDB`) is cleaner and more explicit.
3. **`?_foreign_keys=on` is required** in the SQLite DSN per the upstream example — sqlstore relies on FK cascades for the device store schema.
4. **`waLog.Logger`, not `*slog.Logger`.** whatsmeow predates slog; pass it a `waLog.Logger` (or a small adapter that forwards to slog). Don't try to give it `slog.Default()` directly.
5. **Pairing flow is single-shot per device.** First boot: `client.Store.ID == nil` → call `client.GetQRChannel(ctx)` *before* `client.Connect()`, print the QR to stdout (or render with `github.com/mdp/qrterminal/v3` for terminal scanning), then `Connect()`. Subsequent boots: `Store.ID != nil` → just `Connect()`. The roadmap should split "first-run pair" and "steady-state run" as distinct phases.
6. **Event handler is a single `func(evt interface{})`.** Use a type-switch on `*events.Message`, `*events.Connected`, `*events.Disconnected`, `*events.LoggedOut`, `*events.HistorySync`, `*events.PairSuccess`. Plan for at least the disconnect/reconnect and logged-out paths in v1 — the bot should not crash on a 24-hour reconnect.
7. **Threaded replies = `ContextInfo` with `StanzaID` + `Participant`** of the message being replied to. Build a `*waE2E.Message` with `ExtendedTextMessage.ContextInfo` set; *don't* use `ReactionMessage` (that's emoji reactions, not threaded replies).
8. **Quoted-message body lives at `evt.Message.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage()`.** Required for the "match against quoted text" requirement — easy to miss when first wiring matchers.
9. **`HistorySync` will fire on first pair and dump weeks of messages.** Ignore events whose `evt.Info.Timestamp` predates bot start, or you'll trigger matchers on historical SEFAZ rants.
10. **The device store does in-place schema upgrades on `container.Upgrade(ctx)`.** A whatsmeow version bump that introduces a schema migration will mutate your SQLite file. Back up `data/whatsmeow.db` before each upgrade.

## Stack Patterns by Variant

**If you ever need SQLite extensions (FTS5 custom tokenizer, custom C funcs):**
- Switch to `mattn/go-sqlite3` + `distroless/base-debian13` (has glibc)
- Build with `CGO_ENABLED=1` in a `golang:1.26.3-bookworm` (not alpine) stage to avoid musl/glibc symbol mismatch
- Cost: ~30 MB image, slower CI

**If you outgrow single-group and add a second bot instance:**
- Move whatsmeow store from SQLite to Postgres (sqlstore already supports `"postgres"` dialect)
- Use `github.com/jackc/pgx/v5/stdlib` as the `database/sql` driver
- Bot still ships on `distroless/static` (pgx is pure Go)

**If you add LLM-generated replies (out of scope for v1, but planning ahead):**
- Add `github.com/sashabaranov/go-openai` or `github.com/anthropics/anthropic-sdk-go`
- Both are pure Go; no Dockerfile changes
- Add request-level timeout via `context.WithTimeout` and a circuit breaker (e.g. `github.com/sony/gobreaker`) before shipping

## Version Compatibility

| Package | Compatible With | Notes |
|---|---|---|
| `go.mau.fi/whatsmeow@v0.0.0-20260516…` | Go 1.25+ (toolchain 1.26.3) | Module declares `go 1.25.0`; project's `go 1.26.3` is fine. |
| `modernc.org/sqlite v1.50.1` | Go 1.21+, all GOARCH (amd64/arm64/etc.) | Bundles SQLite 3.53.1. Pure Go — no GOOS/CGO restrictions. |
| `goccy/go-yaml v1.19.2` | Go 1.20+ | No transitive deps. |
| `agnivade/levenshtein v1.2.1` | Go 1.18+ | Strings up to 65 536 runes (well above any WhatsApp message). |
| `fsnotify v1.10.1` | Go 1.21+ | Linux inotify, macOS FSEvents, Windows ReadDirectoryChangesW. Watch the *parent dir*. |
| `testify v1.11.1` | Go 1.19+ | `http` subpkg deprecated → use `net/http/httptest`. Skip `suite` (no parallel). |
| `gcr.io/distroless/static-debian13:nonroot` | Any static linux binary | Includes `ca-certificates`, `/etc/passwd` (nonroot uid 65532), `tzdata`, `/tmp`. |

## Sources

| Source | Verified | Confidence |
|---|---|---|
| https://pkg.go.dev/go.mau.fi/whatsmeow — module overview, example, version `v0.0.0-20260516102357-8d3700152a69` | Yes (WebFetch) | HIGH |
| https://github.com/tulir/whatsmeow `go.mod` via `gh api` — declares `go 1.25.0` / `toolchain go1.26.3` | Yes (gh CLI) | HIGH |
| https://pkg.go.dev/go.mau.fi/whatsmeow/store/sqlstore — `New(ctx, dialect, address, log)` signature; "sqlite3" / "postgres" dialects; `?_foreign_keys=on` requirement | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/modernc.org/sqlite — v1.50.1 (May 10 2026), driver name `"sqlite"`, CGO-free | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/github.com/mattn/go-sqlite3 — v1.14.44 (Apr 29 2026), explicitly requires `CGO_ENABLED=1` + `gcc` | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/gopkg.in/yaml.v3?tab=versions — frozen at v3.0.1 (May 27 2022) | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/github.com/goccy/go-yaml?tab=versions — v1.19.2 (Jan 8 2026), monthly releases | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/github.com/agnivade/levenshtein — v1.2.1 (Dec 23 2024), single `ComputeDistance` func, rune-safe | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/github.com/hbollon/go-edlib — v1.7.0 (Aug 2025), multi-algo, `FuzzySearch` | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/github.com/fsnotify/fsnotify — v1.10.1 (May 4 2026), documented atomic-rename caveat | Yes (WebFetch) | HIGH |
| https://pkg.go.dev/github.com/stretchr/testify — v1.11.1 (Aug 27 2025), v1 maintenance mode | Yes (WebFetch) | HIGH |
| https://github.com/GoogleContainerTools/distroless — `static-debian13` (~2 MiB, ca-certs + tzdata) recommended for CGO-free Go | Yes (WebFetch) | HIGH |

---
*Stack research for: single-binary Go WhatsApp bot (whatsmeow + SQLite + YAML + Docker)*
*Researched: 2026-05-22*
