# Task Backlog

## Queue
<!-- BACKLOG_START -->
- [ ] Clean up stale legacy builtin metadata in `cmd/tabula/kernel.tools.json` and related tests, or formally mark it non-runtime-only with guardrails | Priority: high | Source: reflection-skill-plugin-architecture
- [ ] Add/authenticate or document strict locality for `GET /internal/snapshot/plugins`; verify bind/origin assumptions for non-local deployments | Priority: high | Source: reflection-skill-plugin-architecture
- [ ] Complete external `tabula-bundles` migrations: hook plugins, MCP plugin, driver/subagent plugins, gateway plugins, and remaining per-call skills with `tools[].exec` | Priority: high | Source: reflection-skill-plugin-architecture
- [ ] Implement subagent plugin-side spawn-token/MaxChildren/depth coverage, then remove D1.11(b) dead code and skipped kernel spawn tests | Priority: high | Source: reflection-skill-plugin-architecture
- [ ] Finish Phase 6 SDK/lib relocation: remove in-repo `skills/_pylib` and `skills/_tslib`, remove distro `_pylib`/`_tslib` preserve behavior, and replace inline `_*` support-dir compatibility once bundled wheel/tarball packages exist | Priority: high | Source: reflection-skill-plugin-architecture
<!-- BACKLOG_END -->
