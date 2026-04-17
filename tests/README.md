# Testing Matrix

`T0-11` splits the verification surface into explicit layers so we can run the right checks for the right change.

## Layers

| Layer | Scope | Primary command |
| --- | --- | --- |
| `unit` | Fast logic-only tests in `tests/` plus lightweight Go checks in `cmd/tabula` | `make test-unit` |
| `smoke` | Minimal runtime path: boot, connect, init, message, done | `make test-smoke` |
| `e2e` | Heavier runtime flows: hooks, MCP, observer, subagents, mock driver | `make test-e2e` |
| `contract` | Protocol and extension contract checks under `skills/` | `make test-contract` |
| `manual` | Real-env or diagnostic helpers, not part of the default matrix | run directly |

## Current classification

- `unit`: fast logic-only modules in `tests/` such as `tests/test_boot.py`, `tests/test_compaction.py`, `tests/test_gateway_api.py`, `tests/test_slash_commands.py`, `tests/test_system_prompt.py`, `tests/test_telegram_gateway.py`
- `smoke`: `tests/test_runtime_smoke.py`
- `e2e`: `tests/test_hooks_e2e.py`, `tests/test_mcp_e2e.py`, `tests/test_mock_driver_e2e.py`, `tests/test_observer.py`, `tests/test_openai_subagent_e2e.py`, `tests/test_subagent_e2e.py`
- `contract`: `skills/lib/test_protocol.py`, `skills/hook-permissions/test_permissions.py`
- `manual`: `tests/test_hooks_real.py`, `tests/test_real_subagent.py`, `tests/test_*_diag.py`

## Notes

- `conftest.py` assigns Python test markers automatically by file, so we can split layers without rewriting every existing test module.
- `tests/runtime_harness.py` is a shared helper for future runtime/lifecycle regression suites from `T0-12`.
- `scripts/test-go.sh smoke` still depends on local socket bind permissions because `internal/kernel` tests use `httptest`.
