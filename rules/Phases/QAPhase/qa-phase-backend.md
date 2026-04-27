# QA Phase 4.5 — Backend Category Checklist

## Required Checks

- **Smoke command**: run the project's test/build script if present (`tests/smoke-tests.sh`, `npm test`, `pytest`, `go test ./...`, etc.).
- **API/CLI probe**: exercise the modified endpoint or CLI entrypoint with representative input.
- **Server/log review**: inspect logs produced during smoke/probe for stack traces or HTTP 5xx.

## Evidence (per attempt)

Saved under `memory-bank/qa/artifacts/[task-id]/attempt-[N]/`:

- `smoke.log` — stdout/stderr of the smoke command
- `probe.log` — request/response of API or CLI probe (include command and HTTP status)
- `server.log` (if applicable) — excerpts of server logs during the test window
- `notes.md` (optional context)

## Pass / Fail Gate

| Severity | Examples | Effect |
|---|---|---|
| Blocking | Non-zero exit on smoke command; HTTP 5xx on probe; stack traces in logs | Verdict = `FAILED` |
| Warning | Lint/type warnings; slow-but-successful responses | Logged, non-blocking |

## Quick Category (rare at L3)

For `quick` routed to this subagent: run one minimal smoke command or one curl probe; document rationale in the artifact.

## Degraded Environment Path

If `bash`/`pty_spawn` is unavailable or the command harness cannot run:

1. Record environment restriction in artifact `Environment` field.
2. Mark affected checks as `SKIPPED`.
3. All degraded → Verdict = `SKIPPED`. Partial → Verdict = `PASSED` with explicit warnings.

## Canonical Artifact Schema

The artifact at `memory-bank/qa/qa-[task-id].md` follows the same schema as the visual category (see `qa-phase-visual.md`), but with `Category: backend` (or `quick`) and `QA Agent: 4-5-qa-l3-backend`.

Backend-specific evidence fields to populate:

```markdown
## Evidence
- Commands run: [exact commands]
- Smoke exit code: [0 | N]
- Probe request: [method URL/args]
- Probe response: [status, relevant body/stderr]
- Server/log observations: [excerpts]
- Logs/artifact paths: [relative paths under artifacts/[task-id]/attempt-[N]/]
```
