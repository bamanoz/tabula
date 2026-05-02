# Audit Scope

- Base commit: `77953ca89eada433d601fbf74495b532af7cf687`
- Canonical report: `memory-bank/security/security-execute-remote-runtime-program.md`
- BUILD task scope audited from `memory-bank/progress.md` Agents 1-16 plus `activeContext.md` working set.
- Changed source areas audited:
  - `internal/runtime/**` Runtime API wire, codec, connection, auth, mock, transport.
  - `cmd/tabula-runtime/**` runtime daemon, config, dialer, manifest, worker pool, bare `os/exec` policy.
  - `internal/kernel/runtime_attach.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/snapshot.go`, `internal/kernel/kernel.go`.
  - `cmd/tabula/main.go`, `cmd/tabula/status.go`, associated tests.
  - `go.mod`, `go.sum` dependency delta.
- New dependency introduced: `github.com/coder/websocket v1.8.14`.
- Security-relevant surfaces audited: Runtime API Hello bearer auth, local unix runtime listener, internal runtime snapshot endpoint, status CLI loopback fetch, runtime config/dialer token handling, plugin manifest discovery, worker process spawn boundary, worker pool tenant keying.
