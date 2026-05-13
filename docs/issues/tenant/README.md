# Issues — remote runtime program

This directory holds independently-grabbable issues for the
remote runtime / multi-backend execution program.

**Start here:** `TASK.md` — the multi-agent execution brief.
Read it before grabbing any issue.

- Plan: `docs/plans/REMOTE_RUNTIME.md`
- ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md`
- **Amendments: `AMENDMENTS.md` (authoritative; overrides
  any conflicting issue text)**

A review pass after the M1-M6 cut found 8 critical and ~18
medium-severity inconsistencies. Rather than rewrite every
issue, decisions were recorded in `AMENDMENTS.md`. Implementers
must read it alongside any issue file.

Companion issues added during the review pass:

| ID       | Title                                              | Repo            |
|----------|----------------------------------------------------|-----------------|
| M2-04b   | gateway-telegram plugin SDK migration              | tabula-distrib  |
| M4-03b   | Bundle SDK: tenant-aware state paths               | tabula-bundles  |
| M4-05b   | Bundle SDK: tenant env defense-in-depth check      | tabula-bundles  |
| M4-08    | Kernel-side runtime registry config                | tabula          |
| M5-04b   | Bundle SDK: config template resolver               | tabula-bundles  |

---

## M1 — Runtime API skeleton + interfaces

No behavior change. Establishes the contract.

### Sequencing

```
M1-01 ─┬─▶ M1-02 ───┐
       │            │
       ├─▶ M1-03 ──┬┴──▶ M1-05 ──┐
       │           │              │
       │           ├──▶ M1-04     │
       │           │              ▼
       └───────────┴──────────▶ M1-06
```

| ID    | Title                                            | Repo            | Blocked by         |
|-------|--------------------------------------------------|-----------------|--------------------|
| M1-01 | Wire protocol types (Runtime API)                | tabula          | —                  |
| M1-02 | Worker protocol types (runtime ↔ worker)         | tabula          | M1-01              |
| M1-03 | Go interfaces: RuntimeConn and Backend           | tabula          | M1-01              |
| M1-04 | PluginExecPolicy interface (runtime-side)        | tabula          | M1-02              |
| M1-05 | Mock RuntimeConn for kernel unit tests           | tabula          | M1-03              |
| M1-06 | In-memory end-to-end protocol round-trip test    | tabula          | M1-01, 02, 03, 05  |

### Definition of done for M1

- [ ] `go build ./...` clean.
- [ ] `go test -race ./...` green.
- [ ] No production behavior change.
- [ ] All Go interfaces from §1.7 vocabulary exist with full
      docstrings.
- [ ] Mock-driven unit tests cover every error code and
      lifecycle event from §7 decisions.
- [ ] In-memory end-to-end test asserts the wire contract is
      coherent.

---

## M2 — `tabula-runtime` daemon + atomic stdio removal

Extracts the runtime daemon, ships token auth, replaces the
kernel-side stdio plugin transport in one atomic change.
Skill execution intentionally remains via `process_manager.go`
until M3.

### Sequencing

```
M2-01 ──▶ M2-02 ──▶ M2-03 ──┐
  │         │                │
  │         └──▶ M2-05 ──┐   │
  │                       │  │
  │         ┌─────────────┘  │
  │         ▼                ▼
  │       M2-06 ──────────▶ M2-07 ──▶ M2-08
  │                          ▲
  └──────────────────────────┘

  M2-04 (tabula-bundles) ────▶ M2-07
```

| ID    | Title                                                  | Repo            | Blocked by             |
|-------|--------------------------------------------------------|-----------------|------------------------|
| M2-01 | Wire codec + unix socket transport                     | tabula          | M1-06                  |
| M2-02 | `tabula-runtime` binary skeleton                       | tabula          | M2-01, M1-04           |
| M2-03 | Worker spawn via bare PluginExecPolicy                 | tabula          | M2-02                  |
| M2-04 | Plugin SDK switches to worker protocol                 | tabula-bundles  | M1-02                  |
| M2-05 | Token auth handshake                                   | tabula          | M2-02                  |
| M2-06 | `tabula status` subcommand (JSON output)               | tabula          | M2-02, M2-05           |
| M2-07 | Atomic cutover: local backend + delete stdio + bootstrap | tabula        | M2-01..06              |
| M2-08 | CI integration: build matrix + smoke                   | tabula          | M2-07                  |

### Definition of done for M2

- [ ] `tabula-runtime` binary builds and ships in releases.
- [ ] `tabula serve` forks runtime as managed child; plugin
      tool calls flow kernel → runtime → worker end-to-end.
- [ ] Kernel-side stdio plugin transport physically deleted
      (no legacy code paths).
- [ ] `process_manager.go` skill exec untouched (intentional —
      M3's job).
- [ ] Token auth enforced; tests cover unauthorized,
      missing-token-file, regenerate-on-restart cases.
- [ ] `tabula status --json` works; `bootstrap.sh` uses it.
- [ ] CI green: build matrix + race + runtime-smoke + testbed.
- [ ] All migrated bundle plugins answer Invoke through the new
      pipe.

---

## M3 — Unified worker model

Skills migrate to runtime via auto-generated harnesses; cold
worker mode lands; `process_manager.go` deleted.

### Sequencing

```
M3-01 ──▶ M3-02 ──┬─▶ M3-03 ──▶ M3-05 (bundles)
                  │
                  ├─▶ M3-04
                  │
                  └─▶ M3-06 ──▶ M3-07 ──▶ M3-08 ──▶ M3-09
```

| ID    | Title                                              | Repo            | Blocked by             |
|-------|----------------------------------------------------|-----------------|------------------------|
| M3-01 | Skill manifest reader (runtime side)               | tabula          | M2-03                  |
| M3-02 | Bash skill harness                                 | tabula          | M3-01, M2-03           |
| M3-03 | Python skill harness                               | tabula          | M3-02                  |
| M3-04 | Node skill harness                                 | tabula          | M3-02                  |
| M3-05 | Python skill SDK + bundle migration                | tabula-bundles  | M3-03                  |
| M3-06 | Cold worker mode in the runtime pool               | tabula          | M3-02                  |
| M3-07 | Kernel routes skill calls through Runtime API      | tabula          | M2-07, M3-01..06       |
| M3-08 | Delete `process_manager.go`                        | tabula          | M3-07                  |
| M3-09 | Testbed coverage for unified worker model          | tabula          | M3-08                  |

### Definition of done for M3

- [ ] Bash, Python, Node skill harnesses ship in
      `tabula-runtime`.
- [ ] All existing python skills run end-to-end through the
      python harness; opportunistically migrated to
      `tabula_skill_sdk` where it reduces boilerplate.
- [ ] `cold` worker mode honored: skill workers spawn fresh
      per Invoke, exit after one result.
- [ ] Kernel routes every skill tool call through
      `RuntimeConn.Invoke`; no `process_manager.go` calls left.
- [ ] `process_manager.go` deleted; CI lint guard forbids
      `exec.Command` anywhere in `internal/kernel/`.
- [ ] Skill author surface unchanged (still: read stdin JSON,
      write stdout JSON, exit).
- [ ] Testbed suites exercise cold execution, failure modes,
      and concurrency under installed layout.

---

## M4 — Multi-tenant kernel

Per-tenant config overlays, on-disk layout, three-layer
enforcement, installer fan-out per tenant.

### Sequencing

```
M4-01 ──┬─▶ M4-02 ──┐
        │           │
        ├─▶ M4-03 ──┴─▶ M4-04 ──┐
        │                        │
        │           M4-03 ──▶ M4-05 ──┐
        │                              │
        │                              ▼
        └────────────────────────▶ M4-06 ──▶ M4-07
```

| ID    | Title                                                | Repo            | Blocked by             |
|-------|------------------------------------------------------|-----------------|------------------------|
| M4-01 | Tenant data model + on-disk layout                   | tabula          | —                      |
| M4-02 | `tabula tenant` CLI subcommands                      | tabula          | M4-01                  |
| M4-03 | Per-tenant config overlay + tenant_id plumbing       | tabula          | M4-01, M4-02           |
| M4-04 | Per-tenant installer fan-out                         | tabula          | M4-01, M4-03           |
| M4-05 | Runtime-side tenant whitelist + enforcement          | tabula          | M4-03, M3-06           |
| M4-06 | Bootstrap + `tabula status` for multi-tenancy        | tabula          | M4-02, M4-04, M4-05    |
| M4-07 | Multi-tenant testbed coverage                        | tabula          | M4-01..06              |

### Definition of done for M4

- [ ] Tenants are first-class on disk under
      `$TABULA_HOME/tenants/<id>/` with their own config,
      state, cache, logs.
- [ ] `tabula tenant {list,create,delete,show}` ship with
      `--json` outputs.
- [ ] Every Invoke carries a real tenant_id; kernel router,
      runtime pool, and worker SDK all enforce it (three
      layers, ADR §6).
- [ ] Per-tenant config overlay merges global + tenant
      correctly.
- [ ] Installer fans bundles into every tenant; tenant create
      auto-fans the active generation.
- [ ] `tabula status --json` reports per-tenant capabilities;
      bootstrap.sh creates project tenants.
- [ ] No cross-tenant data leak observed in testbed.
- [ ] Legacy single-tenant `$TABULA_HOME` migrates cleanly to
      `tenants/default/` on first boot (one-shot shim,
      removal milestone documented in source).

---

## M5 — Workspace decomposition

`fs` and `exec` plugins replace the legacy `files` / `shell` /
`workspace` skills. `Hub.ProjectRoot` deleted; workspace lives
in per-tenant config templates.

### Sequencing

```
M5-01 ──┬──▶ M5-03 ──┐
        │            │
M5-02 ──┤            ├──▶ M5-05 ──▶ M5-06
        │            │
        └─▶ M5-04 ───┘
```

| ID    | Title                                                    | Repo            | Blocked by             |
|-------|----------------------------------------------------------|-----------------|------------------------|
| M5-01 | `fs` plugin: roots-aware filesystem operations           | tabula-bundles  | M2-04                  |
| M5-02 | `exec` plugin: cwd-aware subprocess execution            | tabula-bundles  | M2-04                  |
| M5-03 | Workspace boundary hook migration                        | tabula-bundles  | M5-01                  |
| M5-04 | Per-tenant workspace config + delete `Hub.ProjectRoot`   | tabula          | M4-03, M5-01, M5-02    |
| M5-05 | Atomic deletion: `files`, `shell`, `workspace`           | tabula-bundles  | M5-01..04              |
| M5-06 | Workspace decomposition testbed coverage                 | tabula          | M5-05                  |

### Definition of done for M5

- [x] `workspace/fs` plugin owns every fs tool the agent
      uses; lexical roots enforcement, structured
      `fs_outside_root` errors.
- [x] `workspace/exec` plugin owns shell execution;
      independent of fs.roots per Q3.4c; supports background
      processes.
- [x] `${project_root}`, `${tenant_id}`, `${tabula_home}`
      config templates resolve at plugin SDK config-load time.
- [x] `Hub.ProjectRoot`, `TABULA_PROJECT_ROOT`,
      `init.meta.project_root` all physically gone.
- [x] `files/files`, `base/shell`, `code/workspace` skills
      physically deleted from `tabula-bundles`.
- [x] `code/hook-workspace-boundary` migrated or deleted
      based on audit.
- [x] Distros updated to pin fs/exec instead of legacy
      skills.
- [x] Testbed suites cover roots, tenant divergence, exec
      independence, missing-project-root fallback.
- [x] Cross-repo grep guards pass.

---

## M6 — Remote backends

WSS + mTLS + SSH backends, service install for launchd /
systemd, operator documentation. Closes the program.

### Sequencing

```
M6-01 ──┬──▶ M6-02 ──┐
        │            │
        ├──▶ M6-03 ──┤
        │            ├──▶ M6-05
        │            │
        └──▶ M6-04 ──┘
```

| ID    | Title                                                  | Repo            | Blocked by             |
|-------|--------------------------------------------------------|-----------------|------------------------|
| M6-01 | WSS transport (kernel listener + runtime dialer)       | tabula          | M2-01                  |
| M6-02 | mTLS auth (opt-in second mode)                         | tabula          | M6-01, M2-05           |
| M6-03 | SSH backend (out-of-process system `ssh`)              | tabula          | M2-01, M2-02           |
| M6-04 | Service install: launchd + systemd                     | tabula          | M2-06, M2-07           |
| M6-05 | Remote backend operator docs + testbed                 | tabula          | M6-01..04              |

### Definition of done for M6

- [ ] WSS transport ships; kernel can accept off-host runtime
      connections.
- [ ] mTLS opt-in coexists with bearer token; layered defense
      verified in tests.
- [ ] SSH backend ships using system `ssh` (no in-process
      libssh); user SSH config / agent / jump hosts work
      transparently.
- [ ] `tabula runtime token issue/list/revoke` ship with
      hashed on-disk store.
- [ ] `install-service.sh` registers `tabula serve` on launchd
      / systemd and survives reboot.
- [ ] Operator docs cover deployment topologies, security
      model, troubleshooting.
- [ ] Testbed exercises wss + mTLS + ssh + multi-backend
      simultaneously.
- [ ] Docker / k8s backends explicitly **not** in M6 (future
      program).
