# ADR 0024 — Skills own sandboxed authoring tools

- Status: Accepted
- Date: 2026-08-02
- Supersedes: ADR 0020 filesystem permission consequence for installed skill reads

## Context

Installed skills are immutable runtime artifacts under `$TABULA_HOME/skills`, while external skills are user-authored directories discovered from tenant configuration and workspace conventions. Previously agents relied on generic `fs_*` roots to read installed skills and edit external skills. That coupled skill discovery and authoring to an optional filesystem bundle, exposed tenant and distro skill paths through generic filesystem policy, and could not advertise writable authoring locations before the first skill existed.

Skills contain more than `SKILL.md`: references, scripts, assets, and arbitrary supporting files are part of the authoring surface. A high-level CRUD API over skill metadata would hide those resources and duplicate the skill catalog already injected into prompts.

## Decision

The `extensions:skills` plugin owns four filesystem-shaped tools:

- `skill_read` reads files or lists one directory;
- `skill_write` atomically creates or replaces files;
- `skill_edit` performs exact text replacement;
- `skill_delete` removes files or, when explicitly recursive, directories.

Paths are absolute and must remain inside registered skill roots. Installed runtime roots are read-only. Configured external roots and conventional workspace skill roots are writable, including when they do not exist yet, so an agent can create the first skill. Traversal and symlink escape are rejected.

`skill_write` and `skill_edit` validate `SKILL.md`: YAML frontmatter must provide `name` and `description`, `name` must be a valid skill identifier, and it must match the containing directory. Supporting files have no skill-specific content restrictions.

Configured `external_roots` are exact skill directories. Workspace discovery separately registers `workspace/skills` and `workspace/.agents/skills`.

The skills prompt hook always identifies the read-only runtime root and writable external authoring roots. The detailed installed-skill catalog remains the discovery mechanism; no separate list, glob, scope, opaque-ID, or validation tools are added.

Distro materializers do not add runtime, tenant, or external skill directories to generic `fs` roots. Distro permission policy applies path rules to `skill_*` instead.

## Consequences

- Skill management works without the filesystem bundle.
- Agents can manage `SKILL.md`, references, scripts, and assets through one sandboxed surface.
- Runtime skills cannot be modified through authoring tools or through generic distro-provided filesystem roots.
- Empty authoring roots remain visible and usable.
- External root configuration no longer treats a configured parent as an implicit source of nested conventions; callers must register exact skill directories.
- Gateways continue using the shared skill catalog for slash commands; this decision does not change command execution semantics.
