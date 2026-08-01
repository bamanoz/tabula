# ADR 0013 - Bundle dependencies resolve as pinned component closure

Date: 2026-07-29
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

Bundle manifests could require SDK packages from another bundle, but installer
validation required every dependency bundle to be repeated in `distro.toml`.
That made capability bundles non-composable: selecting one capability did not
install its supporting components, and every distro had to duplicate dependency
graph knowledge.

Ouroboros handles capability prerequisites as one Python environment and
revalidates imports after source changes. Tabula instead installs immutable,
component-oriented distro generations. Its dependency contract therefore needs
to preserve source revisions, component allowlists, conflict detection, and
installed fan-out rather than copy Python package installation behavior.

## Decision

`bundle.toml` `[[dependencies]]` entries may declare `components` in addition
to Python and TypeScript package exports:

```toml
[[dependencies]]
bundle = "collaboration"
components = ["sessions"]
python_packages = ["tabula_session_sdk"]
```

Installer computes deterministic transitive closure in dependency order. A
dependency not explicitly selected by distro resolves as sibling bundle from
same source tree as requiring bundle. For git sources it uses exact resolved SHA
of requiring bundle, never moving ref independently. For local sources it uses
sibling directory in same checkout.

Explicit distro bundle entries remain authoritative. Explicit component
allowlist may satisfy dependency unchanged, but dependency cannot widen it.
Missing required components, excluded selections, source/layout failures,
cycles, export mismatches, and install collisions fail before generation
activation with actionable diagnostics.

Auto-resolved bundles install only union of required components. Package-only
dependencies install no runtime components but still fan out declared package
exports. Explicit bundle without component allowlist retains full-bundle
behavior.

Immutable lock format v5 records selected `components` for every bundle entry,
including auto-resolved dependencies, along with source and resolved revision.
Existing v4 locks migrate on read; next successful install rewrites v5.

Kernel remains unchanged. Dependency resolution belongs entirely to distro
installer and testbed selection/generation tooling.

## Consequences

- Distro can select one capability bundle and receive supporting components
  transitively.
- All bundles in git dependency closure use one pinned commit.
- Bundle authors own reusable dependency graph; distro authors own explicit
  restrictions and overrides.
- Locks expose exact source, revision, version, and selected components.
- Cycles and incompatible component restrictions fail deterministically.
- Existing bundles without dependencies retain current behavior.
- Installed-layout testbeds can select only fixture capability and execute tool
  from transitively installed dependency.
