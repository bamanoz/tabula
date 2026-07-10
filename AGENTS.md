# Tabula Core Working Rules

This repository owns the Tabula kernel, distro installer, testbed runner, docs,
and shared development tooling. Distro-specific product policy belongs in
`tabula-distrib`; bundle/plugin/skill implementations belong in
`tabula-bundles`.

## Boundaries

- Keep kernel changes generic. The kernel should not know about concrete
  distros, bundles, skills, plugins, gateways, MCP servers, workspaces, or
  product-specific policy.
- Keep the kernel dumb: it is a router, message bus, lifecycle coordinator, and
  persistence boundary only. Do not add product semantics, UI text, default
  choices, policy decisions, tool-specific payload shaping, or gateway/client
  behavior to kernel code. Put smart behavior in the harness around the kernel:
  plugins, hooks, gateways, clients, distro boot/materializers, and tests that
  exercise those layers.
- Put distro policy in distro boot/materializer code, not in the kernel.
- Put reusable Python runtime helpers in shared bundle libraries when multiple
  distros or components need the same behavior.
- Do not put distro-specific assumptions into this repo's docs or tests unless
  the document is explicitly about distro integration.

## Config And Runtime

- Treat `TABULA_HOME` as runtime/config/state root, not as a user workspace.
- Do not hardcode `~/.tabula` in user-facing text except when documenting the
  default value of `TABULA_HOME`.
- Plugin runtime config uses `config/global.toml`, user-owned
  `config/plugins/<plugin-id>/config.toml`, and installer-compiled tenant
  `tenants/<tenant>/config/plugins/<plugin-id>/config.toml`; do not add new
  `plugin.toml` runtime config blocks.
- Keep `tabula-guide` in `tabula-bundles/base/tabula-guide` current whenever
  runtime layout, config semantics, tool access, skills, plugins, tenants,
  installation, or troubleshooting behavior changes. This guide is the primary
  installed reference that lets agents understand and debug Tabula itself.

## No Legacy / No Backward Compatibility

- Do not preserve legacy code paths, deprecated aliases, or backward-compat
  shims by default. When you rename, restructure, or replace something,
  delete the old surface in the same change and update every caller, test,
  and doc.
- Do not introduce new "legacy alias" constants, fields, methods, env vars,
  config keys, or wire-format aliases. One name per concept.
- Migration shims are allowed only when there is a concrete migration
  requirement (e.g. on-disk lock-file or `TABULA_HOME` layout that already
  exists in user installs). When a shim is required, scope it narrowly,
  document why it exists, and remove it as soon as the migration window
  closes.
- When asked to remove legacy code, remove it everywhere: source, tests,
  docs, comments, and follow-up plan files. Do not leave dangling references.

## Tests

- Bug fixes must be evidence-driven. Reproduce the reported behavior first and
  base the fix on concrete evidence from tests, logs, traces, runtime state, or
  another observable monitoring source. If existing diagnostics are insufficient,
  add targeted temporary or permanent instrumentation, restart the affected
  runtime/gateway/kernel as needed, and verify the live scenario before declaring
  the bug fixed.
- Write production-ready code on the first pass. Do not leave known race risks,
  cleanup gaps, TODO behavior, best-effort protocol validation, or unverified
  edge cases for a later hardening pass.
- If a regression requires installed layout to reproduce, add or update a
  testbed suite. Unit tests alone are not enough for install/fan-out bugs.
- Testbed checks should execute the relevant installed tool/plugin, not only
  assert that it appears in the tool catalog.
- Keep canonical testbed files and the generated testbed template in sync.
- Run focused tests first, then the relevant testbed suite. For Go changes,
  focused tests must use `-race -count=1` unless there is a documented reason
  they cannot. Run baseline with `-race -count=1` when touching shared runtime,
  kernel, concurrency, streaming, installer, or session behavior.
- A plain `go test` run is not sufficient verification for production changes
  that touch goroutines, channels, locks, runtime transport, kernel dispatch,
  spool/file lifecycle, hook delivery, or shared mutable state.
- After any build step, inspect `git status --short` and do not include generated
  artifacts such as `dist/`, `build/`, `*.egg-info/`, `__pycache__/`, or `*.pyc`.

## Installer And Artifacts

- Do not commit generated artifacts: `dist/`, `build/`, `*.egg-info/`,
  `__pycache__/`, or `*.pyc`.
- Use `tabula-install`/`tabula-distro` install behavior as the source of truth for runtime
  layout. If a component needs files at runtime, ensure the installer actually
  fans them out.
- After switching the active generation, the installer atomically updates
  `$TABULA_HOME/run/reload.touch`. A live `tabula serve` polls that file and
  calls `Hub.ReloadPlugins` when its mtime changes, so reinstalls take effect
  without a manual kernel restart. Keep this trigger best-effort: a missing or
  unwritable `run/` directory must not fail an install.

## Architecture Decisions

- Maintain ADRs for architecture-level decisions, especially changes to kernel
  contracts, installer behavior, runtime layout, config surfaces, protocol
  semantics, or repo boundaries.
- Treat existing ADRs as immutable decision log entries. Do not rewrite old ADRs
  to match current behavior.
- If a decision changes, add a new ADR and use `Supersedes` / `Superseded by`
  metadata to link the records. Update mutable docs separately for current
  usage.

## Collaboration

- Prefer the smallest correct change.
- Preserve user changes in dirty worktrees.
- Do not commit unless explicitly asked.
- When changing architecture, update the central docs and tests that enforce
  the new rule.
- Keep documentation current with behavior. If code changes user-facing
  behavior, runtime layout, config, installation, or testing expectations,
  update the relevant docs in the same change.

# Go code rules

## Toolchain
- Go version is defined in `go.mod` (`go` directive). Do not bump it without a dedicated PR.
- Dependency manager is Go modules. Do not use `vendor/` unless it is committed.
- After changing dependencies: `go mod tidy` is mandatory, `go.sum` must be committed.

## Commands (run before submitting)
```bash
go build ./...
go test ./... -race -count=1
go vet ./...
golangci-lint run
gofmt -l . | tee /dev/stderr | (! read)   # must be empty
```
In a monorepo, run only against the affected module — not from the root.

## Project layout
- Follow [golang-standards/project-layout](https://github.com/golang-standards/project-layout):
  `cmd/<app>/main.go`, `internal/`, `pkg/` (only if the code is genuinely intended for external use).
- Business logic belongs in `internal/`. Nothing important goes in `pkg/` without a clear reason.
- `main.go` stays thin: flag parsing, dependency wiring, a `run()` that returns `error`.

## Naming
- Packages: short, lowercase, no `_` and no `camelCase`. Avoid `utils`, `common`, `helpers`, `base`.
- Package name matches its directory name.
- Do not stutter the package name in identifiers: `user.New()`, not `user.NewUser()`.
- Single-method interfaces use the `-er` suffix: `Reader`, `Notifier`.
- Acronyms keep consistent casing: `userID`, `httpClient`, `URL` (not `Url`, not `userId`).
- Export only what really needs to be public. Default to lowercase.

## Errors
- Return `error` as the last parameter. Never use panic for expected errors.
- Wrap with context: `fmt.Errorf("fetch user %d: %w", id, err)`.
- Context prefix is lowercase, no trailing period or `\n`.
- Check via `errors.Is` / `errors.As` — do not compare strings or rely on raw type assertions.
- Sentinel errors are exported variables: `var ErrNotFound = errors.New("not found")`.
- Custom errors are types with an `Error() string` method; add `Unwrap()` when you wrap.
- `panic` is acceptable only in `init()` and in genuinely impossible situations.

## Context
- `context.Context` is the **first** parameter, named `ctx`. Never store it in a struct.
- Do not pass a `nil` context. In tests use `context.Background()` or `t.Context()` (Go 1.24+).
- Do not use `context.Value` for required parameters — only for request-scoped data (trace ID, auth).
- Every blocking operation (HTTP, DB, RPC, sleep) accepts and respects `ctx`.

## Concurrency
- Never start a goroutine without a clear way to stop it (ctx, channel, `sync.WaitGroup`).
- The **sender** closes a channel, not the receiver. Never close a channel twice.
- For goroutine groups with errors use `golang.org/x/sync/errgroup`.
- Protect shared state with `sync.Mutex` or channels. Verify with `-race` in CI.
- Do not embed `sync.Mutex` in a value type that gets copied — use a pointer to a struct that holds the mutex.
- `time.After` inside a `select` in a loop leaks; use `time.NewTimer` with `Stop()`.

## Interfaces
- Define interfaces on the **consumer** side, not the producer side. Keep them small.
- Do not create an interface "for the future" — add it when a second concrete type appears or you need a mock.
- Accept interfaces, return concrete types.
- Use `any` instead of `interface{}` (Go 1.18+).

## Structs and methods
- Pointer receiver if: the method mutates, the struct is large, or any other method already uses a pointer receiver. Otherwise value receiver. Do not mix on the same type.
- Group struct fields by purpose, exported fields first.
- Constructors: `New` for the package's main type (`user.New`), `NewXxx` for the rest.
- Optional parameters use functional options, not a struct full of nilable fields.

## Slices, maps, allocations
- Preallocate when length is known: `make([]T, 0, n)`, `make(map[K]V, n)`.
- Do not return `nil` slices/maps from public APIs if the caller will iterate — an empty slice behaves the same and avoids surprises with marshaling.
- Appending into a shared slice is dangerous; copy when needed.

## HTTP / servers
- Always set timeouts: `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`. A default `http.Server{}` without timeouts does not ship to production.
- Use `http.ServeMux` (Go 1.22+) or an explicit router; do not register handlers on `http.DefaultServeMux`.
- Close `resp.Body` — `defer resp.Body.Close()` immediately after the `err` check.
- Graceful shutdown via `srv.Shutdown(ctx)` on signal.

## Logging
- Structured logging via `log/slog`. No `fmt.Println` or `log.Printf` in production code.
- Levels: `Debug`, `Info`, `Warn`, `Error`. Use `Error` only when something actually needs attention.
- Never log secrets, tokens, or PII. Do not log an error and then return it — pick one (the caller that decides what to do is the one that logs).

## Testing
- Tests live next to the code, in the same package. `_test.go` suffix. Black-box tests use the `foo_test` package.
- Names: `TestXxx`, table-driven tests with a `name string` field and `t.Run(tt.name, ...)`.
- Use `t.Parallel()` where safe. Use `t.Cleanup()` instead of `defer` for test resources.
- Standard library + `testing` + `testify/require` is enough. Mocks are hand-written interfaces or `gomock` — no magic.
- Cover the happy path, edge cases, and errors. Do not chase coverage percentages.
- Benchmarks: `BenchmarkXxx`, run with `go test -bench=. -benchmem`.

## Generics
- Use them when they genuinely remove duplication (`Map`, `Filter`, generic containers).
- Do not turn simple code into generics for "universality". One concrete type beats two type parameters.

## Dependencies
- The standard library is the first choice. Justify any new dependency before adding it.
- Do not pull heavy frameworks (gin, echo) when `net/http` + `chi` is enough.
- Major upgrades go in a dedicated PR with a changelog link.

## Forbidden / avoid
- `init()` for business logic (driver registration and similar is fine).
- Global mutable variables.
- `panic`/`recover` as control flow.
- Ignoring errors: `_ = f.Close()` is acceptable only with a comment explaining why.
- `interface{}` parameters in a public API without strong justification.
- Copying `sync.Mutex`, `sync.WaitGroup`, `bytes.Buffer` (use a pointer).

## Agent skills

### Issue tracker

Issues and PRDs live in GitHub Issues for `bamanoz/tabula`; use the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the default triage labels: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: root `CONTEXT.md` when present plus `docs/adr/`. See `docs/agents/domain.md`.
