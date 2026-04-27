# Security Phase 4.7 — L4 Audit Matrix

L4 SECURITY combines static code review, dependency audit, configuration audit, and threat-model alignment.

## Required Checks

### 1. AuthN/AuthZ Review
- Identify all authn/authz surfaces touched in BUILD (new endpoints, middleware, role checks, session handling).
- Verify no anonymous-by-default routes, no hard-coded credentials, no privilege escalation paths.

### 2. Secrets Handling
- Grep for hard-coded secrets / API keys / tokens in changes.
- Verify env-var usage; no `.env` files committed; secrets not echoed to logs.

### 3. Input Validation / Injection Vectors
- SQL: parameterized queries only.
- Command: no `shell: true` with user input; no string concatenation into `exec`/`spawn`.
- Prompt: when LLM input includes user content, ensure delimiter discipline.
- Path: no user-controlled paths into `fs.read*`/`fs.write*` without normalization.

### 4. Untrusted-Data Boundaries
- Trace user input flows into rendering, eval, exec, regex constructors, deserializers.

### 5. Dependency Review
- For each new package added during BUILD: run `npm audit` (if `package-lock.json`) or `pip-audit` (if `requirements.txt` / `pyproject.toml`) read-only.
- If neither tool is available, mark `Degraded: no audit tool available`.

### 6. Logging & PII
- Verify logs do NOT include passwords, tokens, full PII.
- Verify telemetry/error reporting redacts sensitive fields.

### 7. Configuration Drift
- Default-allow flags: confirm they remain default-deny.
- Debug endpoints: not exposed in non-dev configs.
- CORS / CSP changes reviewed.

## Evidence (per attempt)

Stored under `memory-bank/security/artifacts/[task-id]/attempt-[N]/`:

- `audit.log` — `npm audit` / `pip-audit` output if available.
- `grep.log` — secrets/credentials grep transcript.
- `auth-review.md` — short notes on authn/authz surfaces.
- `findings.md` — per-checklist-item verdict and rationale.

## Pass / Fail Gate

| Severity | Trigger | Effect |
|---|---|---|
| Blocking | Any item evaluates to `BLOCKING` | Verdict = `FAILED` |
| Warning | Any item evaluates to `WARNING` (including degraded) | Logged, non-blocking |

## Degraded Environment Path

- Bash unavailable → cannot run audit tools or grep. All applicable items become `WARNING (Degraded)`. If everything is degraded → Verdict = `SKIPPED`.
- Audit tool absent (no `npm`, no `pip-audit`) → dependency check is `WARNING (Degraded)`. Other items proceed normally.

## Canonical Artifact Schema

`memory-bank/security/security-[task-id].md`:

```markdown
# Security Review: [task-id]
Date: YYYY-MM-DD
Attempt: N / 3
Category: deep
Security Agent: 4-7-security-l4
Verdict: PASSED | FAILED | SKIPPED

## Scope
- BUILD changes audited: [files / commits / PR scope]
- New dependencies introduced: [list or none]

## Checklist
| # | Item | Verdict | Notes / Evidence |
|---|---|---|---|
| 1 | AuthN/AuthZ | OK / WARNING / BLOCKING | ... |
| 2 | Secrets | ... | ... |
| 3 | Input validation | ... | ... |
| 4 | Untrusted-data boundaries | ... | ... |
| 5 | Dependency review | ... | ... |
| 6 | Logging / PII | ... | ... |
| 7 | Configuration drift | ... | ... |

## Findings
### Blocking
- [if any] description, file, suggested mitigation

### Warning
- [if any] description, file, suggested mitigation

### Degraded checks
- [if any] dimension and reason

## Re-entry State
- BUILD re-opened: yes/no
- Attempts after this run: N

## Next Phase
- REFLECT | BUILD | HARD_STOP
```
