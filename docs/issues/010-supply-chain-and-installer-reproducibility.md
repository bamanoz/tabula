# Supply Chain And Installer Reproducibility

Priority: Medium

Repos: `tabula`, `tabula-distrib`, `tabula-bundles`

## Problem

Installer and distro sources use mutable or unauthenticated inputs. Release
archives and requirements are downloaded without checksum/signature validation.
Distro manifests reference `main`; MCP defaults use `@latest`; web package
dependencies use `latest` and broad semver ranges. Installer source copying also
dereferences symlinks.

## Evidence

- `scripts/install.sh`: downloads release archives and runtime requirements
  without checksum/signature verification.
- `tabula-distrib/*/distro.toml`: several sources point to
  `git+https://github.com/bamanoz/tabula-bundles.git@main`.
- `tabula-distrib/code/application/apply.py`: MCP commands include
  `@playwright/mcp@latest` and unpinned `uvx duckduckgo-mcp-server`.
- `tabula-bundles/gateways/gateway-web/web/package.json`: dependencies include
  `latest` and broad ranges.
- `tools/tabula-distro/src/tabula_distro/install.py`: `_copytree` uses
  `symlinks=False`, dereferencing source symlinks.

## Impact

Installs and updates are not reproducible or tamper-evident. A malicious or
changed upstream can alter installed executable behavior. Symlink dereference
can copy host file contents into installed generations from malicious sources.

## Proposed Fix

- Publish and verify checksums or signatures for release assets.
- Pin release distro/bundle sources to tags or SHAs.
- Pin default MCP server package versions.
- Commit and enforce web package lockfiles where generated assets are built.
- Preserve or reject symlinks during install source copy; reject symlinks that
  escape source root.

## Acceptance Criteria

- Installer verifies downloaded artifacts before extraction.
- Release distro manifests do not depend on mutable `main` refs.
- Default MCP commands are version-pinned.
- Installer tests cover escaping symlink rejection or preservation semantics.
