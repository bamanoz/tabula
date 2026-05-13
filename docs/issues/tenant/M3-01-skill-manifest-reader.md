# M3-01 — Skill manifest reader (runtime side)

Status: done
Phase: M3
Type: AFK
Repo: tabula
Labels: needs-triage, area/runtime, phase/m3

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§4)

## What to build

Teach `tabula-runtime` to enumerate **skills** alongside plugins
when building its `target_id → manifest` map (M2-03). After
this slice the runtime knows skills exist and what tools they
expose; it does not yet execute them (M3-02..04 add harnesses,
M3-07 wires them into pool/Invoke).

Components:

- `internal/runtime/host/manifest/skill.go`: parse `SKILL.md`
  frontmatter (YAML) per `tabula-bundles/AGENTS.md` Skill
  Authoring section. Extract:
  - `name` → `target_id` (e.g. `target_id = "skill:timer"`).
  - `description` → tool description (skill-level).
  - `tools[]` → list of tool entries: `name`, `description`,
    `params` (JSON schema), `required[]`, `exec` (template
    string).
- Skill target_id namespacing: prefix `skill:` so plugins
  (`plugin:base-mcp`) and skills cannot collide. Document the
  scheme in the manifest package godoc.
- Runtime mode classification: every skill target carries
  `worker_mode = "cold"` (one process per call, exits after
  result). Plugins carry `worker_mode = "warm"`. Stored on the
  manifest struct.
- Language detection: parse the `exec` string's first token
  (`python3`, `bash`, `node`, …) into a `harness_kind`
  enum (`bash` / `python` / `node`). Unknown → log warning,
  skip the tool.
- `ListCapabilities` (M2-02) extended to include skill tools
  with the skill's `target_id`.
- Reload (M2-03 reload path) re-reads SKILL.md files.

Search paths: same install layout the runtime already scans
for plugin manifests, plus `$TABULA_HOME/skills/<name>/SKILL.md`.

Out of scope:
- Harness execution (M3-02..04).
- Routing kernel tool calls to skill targets (M3-07).
- Skill `SKILL.config.json` overlay reading — that lives in
  the skill harness layer, not the manifest layer.

## Acceptance criteria

- [ ] Reading `tabula-bundles/base/timer/SKILL.md` produces a
      manifest with three tools (`timer_start`, `timer_list`,
      `timer_cancel`) and `worker_mode = "cold"`,
      `harness_kind = "python"`.
- [ ] Malformed frontmatter → clear structured error logged,
      that skill skipped, others continue loading.
- [ ] Frontmatter without `tools[]` → skill registered with
      zero tools (still announced in `ListCapabilities`).
- [ ] `ListCapabilities` reports both plugin and skill targets
      with mode and harness kind.
- [ ] Reload picks up newly added SKILL.md files and removes
      deleted ones.
- [ ] Unit tests cover the matrix of every existing skill in
      `tabula-bundles/` parsing without error.
- [ ] Race-clean.

## Blocked by

- M2-03 (manifest map structure to extend)

## Notes

- Frontmatter format is documented in `tabula-bundles/AGENTS.md`
  (Skill Authoring). Treat it as the source of truth — do not
  invent new fields here.
- Python YAML in Go: use `gopkg.in/yaml.v3` (already a likely
  dep — confirm) or whatever the kernel already uses.
- `exec` template variables (e.g. `${SKILL_DIR}` / `${TABULA_HOME}`)
  are NOT expanded here. Expansion is harness's job (M3-02..04).
