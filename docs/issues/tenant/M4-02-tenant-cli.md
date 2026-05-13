# M4-02 — `tabula tenant` CLI subcommands

Status: done
Phase: M4
Type: AFK
Repo: tabula
Labels: needs-triage, area/cli, area/tenancy, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§5, §9)

## What to build

Composable CLI primitives for managing tenants. Per ADR §9,
no prompts, no orchestration, no decoration — data primitives
only. Shell scripts (M4-06 bootstrap update) consume these.

Subcommands:

- `tabula tenant list [--json]`:
  - JSON: `[{"id": "myproject", "display_name": "...",
    "created_at": "..."}]`.
  - Human: tab-aligned `id  display_name  created`.
  - Always exit 0 even when list is empty.

- `tabula tenant create <id> [--display-name "Name"]`:
  - Validates id (M4-01 rules).
  - Creates the tenant directory layout.
  - Idempotent: `--exists-ok` flag → exit 0 if tenant
    already exists; default → exit 1 with structured error.
  - Output: nothing on success (silent for scripting); errors
    on stderr in `code: message` format.

- `tabula tenant delete <id>`:
  - Refuses to delete `default` unless `--force` passed.
  - Refuses to delete a tenant with active sessions unless
    `--force`.
  - Removes the entire `tenants/<id>/` subtree.

- `tabula tenant show <id> [--json]`:
  - Per-tenant detail: id, display name, created_at, plugin
    list, skill list, active session count.

Implementation notes:

- All subcommands talk to the kernel only via the on-disk
  store (M4-01) — no kernel-running requirement. They work
  even when `tabula serve` is down.
- Exception: `--with-runtime` flag on `tenant show` queries
  live runtime via Runtime API for current capabilities. If
  kernel/runtime isn't running, command falls back to disk
  data, marks fields as "stale" in JSON, exits 0.

## Acceptance criteria

- [ ] All four subcommands implemented and tested.
- [ ] `--json` output for `list` and `show` follows documented
      shape.
- [ ] Validation errors surface with clear messages (mismatched
      id format, reserved names, tenant exists, etc.).
- [ ] `default` tenant deletion blocked without `--force`.
- [ ] Snapshot tests for both human and JSON outputs.
- [ ] No prompts, no progress bars, no colors.
- [ ] Race-clean.

## Blocked by

- M4-01 (data model + store)

## Notes

- Naming: `tabula tenant create` not `add`. `delete` not
  `remove`. Keep verbs consistent across CLI.
- Why not `tabula tenants` (plural)? Singular `tenant` matches
  `tabula plugin`, `tabula skill` (whichever exists today).
  Follow the existing convention — verify before merging.
