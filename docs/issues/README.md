# Review Issue Backlog

Security and architecture backlog from the cross-repository Tabula review.

Scope covers:

- `tabula` kernel, runtime API, installer, docs, testbed tooling.
- `tabula-bundles` reusable plugins, gateways, SDKs, drivers, and skills.
- `tabula-distrib` distro policy, boot/materializers, and testbed suites.

Each issue is intended to be independently grabbable. Some issues span multiple
repositories; implement the smallest cross-repo change that closes the risk and
adds focused tests.

## Priority Order

1. `001-kernel-client-auth-and-hook-result-identity.md`
2. `002-approval-response-spoofing.md`
3. `003-enforce-agent-visible-tools.md`
4. `004-mcp-admin-permissions-and-timeouts.md`
5. `005-fs-edit-large-file-data-loss.md`
6. `006-websocket-hardening.md`
7. `007-exec-tool-process-and-policy-hardening.md`
8. `008-subagent-preset-and-state-hardening.md`
9. `009-public-sessions-endpoint.md`
10. `010-supply-chain-and-installer-reproducibility.md`
11. `011-web-gateway-auth-and-frame-hardening.md`
12. `012-memory-tool-permissions.md`
