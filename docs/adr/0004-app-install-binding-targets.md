# ADR 0004 — App install binding targets

Date: 2026-06-23
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

ADR 0002 defines app bindings as the platform selection model and rejects
platform-level workspace/global scopes. That model is still correct: the kernel
and installer must not know distro semantics such as workspaces, prompt files,
skills policy, chats, organizations, or concrete agent products.

However, users still need a simple install-time choice:

- use this app as my user-wide fallback agent;
- use this app for one workspace/directory tree;
- install this app without selecting it automatically.

Without a first-class installer command, users have to compose lower-level
`app apply`, `app prepare`, `app bind --default`, and
`app bind --directory` calls. That makes global installs awkward even though the
binding registry already supports them.

## Decision

Add a generic installer command that materializes an app and applies an optional
binding target:

```bash
tabula-install app install <manifest> --global
tabula-install app install <manifest> --workspace <path>
tabula-install app install <manifest> --no-bind
```

These flags are user-facing install targets, not persisted platform scopes:

- `--global` writes a `default` app binding.
- `--workspace <path>` writes a `directory` app binding for that path.
- `--no-bind` materializes the app and runtime surface without changing app
  selection.

The persisted model remains the binding registry from ADR 0002. The app manifest
continues to carry distro source, launch topology, and distro-owned `values`.
The installer may override the manifest's bindings for this install operation,
but it must record the effective bindings in the applied app lock.

## Boundaries

The installer owns only mechanical app installation:

- resolve and lock the manifest;
- install or refresh the referenced distro source;
- create/update the tenant metadata;
- invoke the distro-owned application materializer;
- compile tenant plugin config;
- write runtime config;
- write app bindings;
- trigger runtime reload.

The installer must not know concrete distro names or product policy. Commands
such as `tabula-install app init claw --global` are not allowed because `claw` is
a distro/product selector, not a generic installer concept.

The distro owns what missing workspace-like values mean. For example, one distro
may treat a missing workspace as a user-level assistant home, another may reject
it, and another may not have a workspace concept at all.

## Consequences

Positive:

- Users can install an app globally or for a workspace with one command.
- The stored selection model stays `default`/`directory` bindings.
- Kernel and installer remain distro-agnostic.
- Distro materializers keep ownership of workspace and prompt semantics.

Negative:

- `--global` is still a potentially misleading UX word; documentation must make
  clear that it means default binding, not platform global scope.
- Manifest bindings and install-time binding overrides can differ, so audit and
  inspect tools must show effective/applied bindings clearly.

## Verification

Installer tests must cover:

- `app install --global` writes a default binding and no directory binding;
- `app install --workspace <path>` writes a directory binding and no default
  binding;
- `app install --no-bind` materializes tenant/runtime state without bindings;
- applied app locks record the effective bindings;
- app runtime config includes the installed tenant.

Distro tests must cover any distro-specific behavior for omitted workspace-like
values.
