# Externalize concrete tool audit enrichment

**Type:** AFK  
**Status:** proposed

## What to build

Remove concrete tool-name and input-schema knowledge from kernel hook auditing. Kernel records generic bounded facts: payload digest, tool identity, call ID, correlation fields, input keys, and input digest.

Optional tool-specific enrichment belongs to the owning security, workspace, subagent, or artifact bundle through a generic audit-enricher contract or producer-owned auxiliary session records.

## Evidence

`internal/kernel/hooks/audit.go:addKnownInputSummary` special-cases `exec_run`, `exec_run_background`, `fs_*`, `tool_result_read`, and `subagent_*`.

## Acceptance criteria

- [ ] Kernel audit code contains no concrete first-party tool names.
- [ ] Generic audit records remain bounded, deterministic, and secret-safe.
- [ ] Required workspace/security/subagent enrichment is implemented by its owning bundle or intentionally deleted.
- [ ] Unknown third-party tools receive the same generic audit treatment as first-party tools.
- [ ] Audit record consumers and `tabula-guide` are updated.
- [ ] Security tests prove no raw command, task, or sensitive payload is newly logged.

## Blocked by

None - can start immediately.
