# Testing Matrix

`T0-11` splits the verification surface into explicit layers so we can run the right checks for the right change.

## Layers

| Layer | Scope | Primary command |
| --- | --- | --- |
| `unit` | Fast logic-only tests for Go kernel/CLI and Python distro installer | `make test-unit` |
| `smoke` | Minimal Go runtime path: connect, join, init, routing, tools, shutdown | `make test-smoke` |
| `e2e` | Reserved for heavier runtime flows owned by distro/bundle repos | `make test-e2e` |
| `contract` | Protocol/SDK contract checks. During library relocation this is temporarily backed by legacy support-dir tests until the packaged SDK contract suite is available. | `make test-contract` |
| `manual` | Real-env or diagnostic helpers, not part of the default matrix | run directly |

## Current classification

- `unit`: `cmd/tabula`, `tools/tabula-distro/tests`, and any fast tests under `tests/`
- `smoke`: selected `internal/kernel` tests run by `scripts/test-go.sh smoke`
- `e2e`: currently empty in this repo; distro/bundle e2e suites live with `tabula-distrib` / `tabula-bundles`
- `contract`: temporary compatibility check in the legacy Python support-dir
  protocol test.
  Replace this with the packaged Python SDK contract suite once the external
  SDK wheel evidence row is green.
- `manual`: ad hoc local checks, not part of the default matrix

## Notes

- `conftest.py` assigns Python test markers automatically by file, so we can split layers without rewriting every test module.
- `scripts/test-go.sh smoke` still depends on local socket bind permissions because `internal/kernel` tests use `httptest`.
