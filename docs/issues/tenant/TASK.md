# TASK — Execute the remote-runtime program (M1 → M6)

You are a multi-agent system tasked with implementing the
remote runtime / multi-backend execution program for Tabula.
This document is your entry point. Read it once in full
before grabbing any issue.

---

## 0. Read these first, in this order

1. **`docs/plans/REMOTE_RUNTIME.md`** — the program plan.
   Sections §1 (inventory), §2 (target shape), §6 (sequencing),
   §7 (decided open questions). Treat as background; do not
   re-litigate the §7 decisions.
2. **`docs/adr/0001-runtime-daemon-and-execution-backends.md`**
   — the architectural decisions: 10 numbered commits, 6
   rejected alternatives. This is **load-bearing**. Every
   issue cites it.
3. **`docs/issues/AMENDMENTS.md`** — **authoritative**
   corrections from a post-cut review pass. Overrides any
   conflicting text in individual issue files. If you skip
   this you will ship inconsistencies.
4. **`docs/issues/README.md`** — milestone overview,
   sequencing graphs, definitions of done.
5. **`docs/issues/M*-*.md`** — the 47 work items.
6. **`AGENTS.md` in each repo you touch** —
   `tabula/AGENTS.md`, `tabula-bundles/AGENTS.md`,
   `tabula-distrib/AGENTS.md`. These contain the working
   rules. Do not violate them.

---

## 1. Repository map

Three repos. Each has its own `AGENTS.md` with binding rules.

| Repo                 | Path                          | Owns                                              |
|----------------------|-------------------------------|---------------------------------------------------|
| `tabula`             | `/Users/mak/src/tabula`       | Kernel, runtime daemon, installer, testbed, CLI, docs, this issue tracker |
| `tabula-bundles`     | `/Users/mak/src/tabula-bundles` | Reusable skills, plugins, clients, shared `_lib` |
| `tabula-distrib`     | `/Users/mak/src/tabula-distrib` | Distro product policy: `claw`, `coder`, gateway plugins |

**Boundaries (binding):**

- Kernel changes are generic. No distro policy in kernel.
- Distro policy lives in `tabula-distrib`. No bundle-internal
  assumptions in distros.
- Reusable runtime helpers in `tabula-bundles/_lib/`.

---

## 2. Philosophy — do not dilute

The architecture is the strongest asset of this project. Hold
the line. Concretely:

### 2.1 Touch the kernel last, only when unavoidable.

The kernel is the smallest, most-tested, most-load-bearing
surface. Almost every M1-M6 issue is solvable without
touching kernel code:

- Wire types, codecs, transports → `internal/runtime/...`
  (new packages).
- Worker spawn, harnesses, pool → `cmd/tabula-runtime/...`
  (new binary).
- Skills, plugins, SDKs → `tabula-bundles/...`.
- Distro policy → `tabula-distrib/...`.

The kernel changes only at well-defined cutover moments:
**M2-07** (delete stdio plugin transport), **M3-07** (route
skills via Runtime API), **M3-08** (delete
`process_manager.go`), **M4-03** (tenant_id plumbing),
**M4-08** (runtime registry), **M5-04** (delete
`Hub.ProjectRoot`).

If your issue isn't one of those, and you find yourself
editing files under `internal/kernel/`, **stop and re-read
your issue**. Either the issue is mis-scoped, or you're
solving the wrong problem.

### 2.2 No legacy. No backward compatibility.

Per `tabula/AGENTS.md` and `tabula-bundles/AGENTS.md`:

- Rename / restructure / replace → delete the old surface in
  the same change. Update every caller, test, doc.
- No "legacy alias" constants, fields, methods, env vars,
  config keys, or wire-format aliases. **One name per
  concept.**
- Migration shims allowed only for concrete on-disk migration
  requirements (e.g. `$TABULA_HOME` layout that already
  exists in user installs). When required, scope narrowly,
  document the removal milestone, remove when the window
  closes. AMENDMENTS.md M15 lists the only such shim
  currently in the program (M4-01).

If an issue text says "delete X", actually delete it. Do not
soft-deprecate. Do not ship a wrapper that calls the new
thing under the old name.

### 2.3 Smallest correct change.

When in doubt, ship the smaller diff. Refactoring opportunities
discovered during an issue are recorded as follow-up issues,
not bundled into the current one.

### 2.4 Skill / plugin / client distinction is real.

- **Skill** = per-call, stateless, cold worker (one process
  per Invoke).
- **Plugin** = long-lived, stateful, warm worker (one process
  per `(kernel, tenant, target)`, reused).
- **Client** = something a human or external system uses to
  talk to the kernel.

Don't blur the lines. If a "skill" needs lifecycle hooks,
shared state, child supervision, dynamic tools, or
subscriptions — it is a plugin, not a skill.
`tabula-bundles/AGENTS.md` Skill Authoring is binding.

### 2.5 Tenant_id is mandatory in every Invoke.

Even in M1/M2/M3 where `default` is the only real tenant.
The wire types validate it. The runtime pool keys on it.
The worker env carries it. Three-layer enforcement (ADR §6)
is non-negotiable.

### 2.6 Composable CLI primitives + shell orchestration (Q9).

Go CLI subcommands are data primitives with `--json` output.
Workflow / prompts / progress / orchestration live in shell
scripts (`bootstrap.sh`, `install-service.sh`, etc.). No
prompts, no decoration, no colors in Go subcommands. Per
ADR §9 — load-bearing.

---

## 3. Tests — testbed first, unit tests second

Per `tabula/AGENTS.md` "Tests" rule:

- Install / fan-out / runtime-layout bugs require **testbed
  coverage**. Unit tests are not enough.
- Testbed checks must execute the relevant installed tool /
  plugin, not just assert it appears in the catalog.
- Canonical testbed files and the generated testbed template
  must stay in sync.

**Workflow per issue:**

1. Write / update unit tests for the new code (focused,
   in-package).
2. Write / update the testbed suite that exercises the
   installed behavior.
3. Run focused tests first.
4. Run the relevant testbed suite.
5. Run the baseline testbed when touching shared runtime or
   installer behavior.

Issues that explicitly add testbed coverage: **M2-08**,
**M3-09**, **M4-07**, **M5-06**, **M6-05**. Treat these as
real gates, not afterthoughts.

If you ship code without the matching testbed update, you
have not finished the issue.

---

## 4. Sequencing and dependency rules

### 4.1 Read the dep graph in `docs/issues/README.md`

Each milestone's section has a sequencing diagram. Follow it.

### 4.2 Don't pick up an issue whose `Blocked by` isn't merged.

If you must work in parallel on something blocked, branch
from the dependency's branch, not from `main`. Merge order
matters.

### 4.3 Atomic cutovers are atomic.

Three issues are riskier than the rest:
- **M2-07** — delete kernel-side stdio plugin transport.
- **M3-08** — delete `process_manager.go`.
- **M5-05** — delete `files`/`shell`/`workspace` skills.

Each requires every prerequisite issue merged. Each touches
multiple repos in lock-step. PR description must list the
prerequisite commit SHAs and the cross-repo PR URLs. Don't
land one repo's portion days before the others.

### 4.4 Cross-repo work

Several issues are cross-repo:
- M2-04 (bundles) ↔ M2-04b (distrib) ↔ M2-07 (tabula)
- M4-03 (tabula) ↔ M4-03b (bundles)
- M4-05 (tabula) ↔ M4-05b (bundles)
- M5-04 (tabula) ↔ M5-04b (bundles) ↔ M5-05 (bundles+distrib)

Coordinate landing. Use the same merge window for paired
PRs. Do not split across release boundaries; an
intermediate state where bundles expect new SDK but kernel
ships old SDK (or vice versa) is a guaranteed prod break.

---

## 5. Per-issue execution checklist

Before opening a PR for an issue:

- [ ] Read the issue file end-to-end.
- [ ] Read `AMENDMENTS.md` and check if any amendment applies.
      Reference the amendment ID in the PR description.
- [ ] Read the `AGENTS.md` for every repo your PR touches.
- [ ] Check the issue's `Blocked by` — all merged?
- [ ] Smallest correct change?
- [ ] No legacy code paths left, no backward-compat shims
      added?
- [ ] If you touched `internal/kernel/`, was the issue
      one of the documented kernel-cutover issues (§2.1)?
      If not, justify in PR description.
- [ ] Unit tests pass.
- [ ] Relevant testbed suite passes.
- [ ] Baseline testbed passes if you touched shared runtime
      or installer behavior.
- [ ] Documentation updated in the same change (per
      `tabula/AGENTS.md` Collaboration rule).
- [ ] Cross-repo PRs linked in description if applicable.
- [ ] PR title prefixes the issue ID: `M2-03: ...`.

---

## 6. Wire / config / error code roster

The following names are canonical (per AMENDMENTS.md C8 / M1
/ M3 / M4). Use exactly these spellings. Do not invent
synonyms.

### Wire-level error codes

```
unauthorized           runtime_unavailable    runtime_busy
unknown_runtime        tenant_unknown         tenant_forbidden
target_unknown         target_forbidden       tool_not_found
timeout                cancelled              protocol_error
internal_error         skill_exec_failed
```

Plugin-internal codes (not wire): `fs_outside_root`,
`exec_denied`. Live in tool result envelopes.

### Config keys

- Kernel side: `$TABULA_HOME/config/global.toml`
  - `[kernel] id`
  - `[[runtime]]` (singular, list-of-tables) — runtime
    registry
- Tenant side: `$TABULA_HOME/tenants/<id>/tenant.toml`
  - `[tenant] allowed_runtimes`, `default_runtime`
  - `[workspace] project_root`
- Runtime side: `$TABULA_HOME/config/runtime.toml`
  - `[[kernel]]` (singular) — kernels to dial

No plurals (`[[runtimes]]`, `[[kernels]]`). No drift.

### Token field name

`token_file = "..."` (path), never `token = "${...}"` to
mean a path.

### Target field shape

Wire form: `{kind: "skill"|"plugin", id: "<name>"}`.
Internal display strings (`"skill:timer"`) are for logs
only, never wire.

### ID validation rule (tenant_id, runtime_id, kernel_id)

`^[a-z0-9][a-z0-9-]{0,62}$`. Reserved names: `default`
(tenants), `system`, `runtime`, `kernel`, `admin`. Validated
at config load and config write.

### Worker env contract

Every worker process sees:
- `TABULA_HOME`
- `TABULA_KERNEL_ID` (sourced from `[kernel] id`)
- `TABULA_TENANT_ID`
- `TABULA_TENANT_DIR` (= `$TABULA_HOME/tenants/<id>/`)
- `TABULA_TARGET_ID` (= e.g. `plugin:fs`, `skill:timer`)
- (skill-only) `TABULA_SKILL_DIR`, `TABULA_TOOL_NAME`,
  `TABULA_CALL_ID`

Missing `TABULA_TENANT_ID` or `TABULA_KERNEL_ID` → SDK
refuses (M4-05b). No fallback.

### Template variable registry

`${tabula_home}`, `${tenant_id}`, `${project_root}`. Two
implementations (Python SDK + Go harness), shared test
corpus enforces consistency (M5-04b).

---

## 7. Where things live (post-program target)

Use this mental model when deciding where a new file goes:

```
tabula/
  cmd/
    tabula/                  # CLI binary (existing)
    tabula-runtime/          # NEW — runtime daemon (M2-02)
      manifest/              # plugin + skill manifest readers
      policy/bare/           # bare PluginExecPolicy (M2-03)
      harness/{bash,python,node}/   # skill harnesses (M3-02..04)
      pool/                  # warm/cold worker pool (M2-03 + M3-06)
      dialer/                # kernel dial logic (M2-02)
  internal/
    runtime/                 # NEW — Runtime API surface
      wire/                  # wire types (M1-01)
      worker/wire/           # worker protocol types (M1-02)
      api.go                 # RuntimeConn, Backend interfaces (M1-03)
      mock/                  # mock RuntimeConn (M1-05)
      codec/                 # WS-framed JSON codec (M2-01)
      transport/{unixsock,wss}/  # transports (M2-01, M6-01)
      backend/{local,ssh}/   # backends (M2-07, M6-03)
    kernel/                  # EXISTING — touch only at cutovers
      tenant/                # NEW — tenant data model (M4-01)
      runtime/registry/      # NEW — runtime registry (M4-08)
      plugin/                # delete stdio path in M2-07
      process_manager.go     # delete in M3-08
  scripts/
    bootstrap.sh             # NEW (M2-07, extended M4-06)
    install-service.sh       # NEW (M6-04)
    tenant-init.sh           # NEW (M4-06)
  tools/
    tabula-distro/           # installer (per-tenant fan-out: M4-04)
    tabula-testbed/          # testbed runner
  docs/
    plans/REMOTE_RUNTIME.md
    adr/0001-...md
    issues/                  # this dir; AMENDMENTS.md authoritative
    operating/               # NEW (M6-05)

tabula-bundles/
  workspace/
    fs/                      # NEW plugin (M5-01)
    exec/                    # NEW plugin (M5-02)
  base/{cron,mcp,sessions,...}/   # plugins (migrated in M2-04)
  code/, caveman/, memory/, ...   # skills (migrated opportunistically in M3-05)
  test-fixtures/             # testbed bundle fixtures
  _lib/python/
    src/
      tabula_plugin_sdk/     # plugin SDK (M2-04, M4-03b, M4-05b, M5-04b)
      tabula_skill_sdk/      # skill SDK (M3-05, M4-03b, M4-05b)

tabula-distrib/
  claw/
    plugins/
      gateway-telegram-plugin/   # migrated in M2-04b
    distro.toml, boot.py, workspace_policy.py
  coder/...
```

Deleted by program end:
- `tabula-bundles/files/files/` (M5-05)
- `tabula-bundles/base/shell/` (M5-05)
- `tabula-bundles/code/workspace/` (M5-05)
- `tabula-bundles/code/hook-workspace-boundary/` (M5-03 audit)
- `tabula/internal/kernel/process_manager.go` (M3-08)
- `tabula/internal/kernel/plugin/runtime.go` stdio path (M2-07)
- `Hub.ProjectRoot`, `TABULA_PROJECT_ROOT`,
  `init.meta.project_root` (M5-04)

---

## 8. Definition of done — the whole program

When all 47 issues + 5 companions merge:

- [ ] `tabula-runtime` binary ships in releases alongside
      `tabula`.
- [ ] Plugin tool calls flow kernel → runtime → worker via
      Runtime API. No `os/exec` for plugins in
      `internal/kernel/`.
- [ ] Skill tool calls flow the same path. No `os/exec` for
      skills anywhere in `internal/kernel/`.
- [ ] CI lint guard: `internal/kernel/` contains zero
      `exec.Command` callsites.
- [ ] Multi-tenant: each tenant has its own state, config
      overlay, and worker process pool. Three-layer enforcement
      live (router + pool + worker SDK).
- [ ] `workspace/fs` and `workspace/exec` plugins replace
      legacy file/shell skills. `Hub.ProjectRoot` deleted.
- [ ] WSS + mTLS + SSH backends ship. Local backend stays the
      default and the testbed default.
- [ ] `install-service.sh` registers `tabula serve` on launchd
      / systemd.
- [ ] Operator docs in `docs/operating/` cover deployment
      topologies, security model, troubleshooting.
- [ ] Testbed exercises full matrix: cold/warm pool,
      multi-tenant isolation, fs roots, mTLS, SSH, multi-
      backend.
- [ ] Documentation matches code in every repo. No
      orphan references to deleted concepts.

---

## 9. When in doubt

- Re-read this file.
- Re-read `AMENDMENTS.md`.
- Re-read the issue's parent ADR section.
- If still in doubt, ask before writing code. The cost of a
  question is far below the cost of an architectural drift
  that has to be unwound later.

The plan is good. The decisions are made. Execute them.
