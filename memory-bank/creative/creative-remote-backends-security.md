# Creative Phase: Remote Backends Security

## 1. PROBLEM DEFINITION
- What needs to be designed: the M6 security posture for WSS, opt-in mTLS, SSH backend, token lifecycle, revocation, and service installation on top of the stable Runtime API.
- Constraints:
  - M6 is additive on the M1-M5 Runtime API; do not redesign worker model or tenant registry.
  - Kernel-side backend config uses singular `[[runtime]]`; runtime-side dial-out config uses singular `[[kernel]]`.
  - Use `token_file` for token paths; do not copy stale `token = "..."` examples unless deliberately introducing a plaintext token value for a separately documented purpose.
  - SSH backend uses out-of-process system `ssh`, not an in-process SSH library.
  - Service install is shell orchestration around `tabula serve`; no Go prompt/bootstrap UX and no `--foreground` flag.
  - Docker/k8s/firecracker backends are future work only.
- Success criteria:
  - BUILD can implement WSS/mTLS/SSH/service slices with consistent auth, config, and revocation semantics.
  - SECURITY can audit token storage, tenant isolation, process boundaries, and operator docs using explicit evidence requirements.
  - Operator docs explain threat model and safe deployment choices without relying on the plan as the only guide.
- Non-functional requirements:
  - Tokens and certs must not leak via logs/status output.
  - Revocation must disconnect active remote runtimes promptly.
  - Service units must run with least surprise and clear log locations.

## 2. OPTIONS

### Option A: Layered auth with backend-specific transport trust
- Description: Keep bearer token as universal authorization; add mTLS as optional identity/second factor for WSS; rely on OpenSSH trust for SSH transport while still using Runtime API token handshake. Service install remains shell-managed.
- Architecture: Kernel registry defines backends; runtime-side `[[kernel]]` defines dial-out WSS/local targets; all backends terminate in the same RuntimeConn and Hello auth flow.
- Advantages:
  - Matches ADR §7 and issue M6-01..05.
  - Clear threat model by transport.
  - Token revocation works across WSS and SSH because it lives above transport.
  - Avoids reimplementing SSH and avoids Go orchestration scope creep.
- Disadvantages:
  - Operators must manage token files and optionally PKI/known_hosts.
  - Two auth artifacts for mTLS deployments: cert plus token.
- Risk factors:
  - Misconfigured WSS without TLS is acceptable for tests but dangerous in production unless docs warn clearly.

### Option B: Replace tokens with mandatory mTLS for all remote backends
- Description: Require certs for remote runtime identity and drop bearer token auth remotely.
- Architecture: WSS/mTLS becomes the primary gate; SSH may skip Runtime API token.
- Advantages:
  - Strong non-transferable identity when PKI is correctly managed.
  - Reduces one class of token-file secret handling.
- Disadvantages:
  - Conflicts with ADR that token mode remains.
  - Makes simple deployments and SSH stdio flow harder.
  - Requires production-grade certificate onboarding before M6 can close.
- Risk factors:
  - Over-scopes M6 and breaks token revoke/list requirements.

### Option C: Plain token-only remote security with no mTLS or operator hardening
- Description: Use bearer tokens over WSS/SSH and defer mTLS/service security docs.
- Architecture: Minimal remote backend security.
- Advantages:
  - Fastest implementation path.
  - Fewer config knobs.
- Disadvantages:
  - Fails accepted M6 scope for opt-in mTLS and operator docs.
  - Weaker production story and poor SECURITY evidence.
  - Does not cover service-install posture.
- Risk factors:
  - Tokens become long-lived bearer secrets without adequate rotation/revocation guidance.

## 3. ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Complexity | 3 | 4 | 2 | 5 |
| Performance | 2 | 5 | 5 | 5 |
| Maintainability | 5 | 5 | 3 | 2 |
| Scalability | 4 | 5 | 4 | 2 |
| Security | 5 | 5 | 5 | 2 |
| **Weighted Total** | | **92** | 70 | 57 |

## 4. DECISION
**Selected: Option A — layered auth with backend-specific transport trust.**

Justification: Option A preserves the accepted two-phase auth model and keeps the Runtime API auth boundary consistent across local, WSS, and SSH transports. It also gives operators a practical path: tokens for simple setups, mTLS for stronger identity, OpenSSH for SSH trust, and shell-managed service installation.

Trade-offs accepted:
- Operators must protect token files and certificate private keys.
- SECURITY must verify both token and certificate paths instead of a single mechanism.

## 5. IMPLEMENTATION GUIDELINES

### Threat model summary
- In scope:
  - Off-host runtime connecting to kernel over WSS.
  - Runtime identity spoofing and token theft.
  - Tenant escape via wrong runtime/tenant routing.
  - Active token revocation and stale runtime disconnect.
  - SSH host/user trust delegated to OpenSSH.
  - Service persistence risks from launchd/systemd units.
- Out of scope for M6:
  - Docker/k8s/firecracker backends.
  - Enterprise IdP/OIDC/JWT discovery.
  - Full OS sandboxing for `exec` on macOS.
  - Automatic SCP upload/install of remote runtime binaries.

### WSS backend posture
- Kernel exposes `/runtime` only when configured.
- WS subprotocol should advertise protocol version, e.g. `tabula-runtime.v1`; mismatch closes with protocol error.
- Production deployments use TLS termination. `ws://` is acceptable only for tests/local loopback and must be documented as non-production.
- Origin allow-list is a browser-origin mitigation; it is not authentication.
- Disconnect semantics remain the Runtime API contract: pending calls fail with `runtime_unavailable`, retryable true.
- Kernel-side registry example:
  ```toml
  [[runtime]]
  id = "laptop-mak"
  backend = "wss"
  url = "wss://kernel.team.example/runtime"
  token_file = "/etc/tabula/runtime-token"
  ca_file = "/etc/tabula/team-ca.pem"
  ```
- Runtime-side dial-out example:
  ```toml
  [[kernel]]
  id = "team"
  url = "wss://kernel.team.example/runtime"
  token_file = "${TABULA_HOME}/run/runtime-token"
  tenants = ["myproject"]
  ca_file = "/etc/tabula/team-ca.pem"
  ```

### mTLS posture
- mTLS is opt-in and complements bearer token; it does not replace token validation.
- Kernel listener modes:
  - `client_auth = "none"`: token only.
  - `client_auth = "request"`: accept cert-presenting and cert-less clients; cert-less still need token.
  - `client_auth = "require"`: reject cert-less clients; valid token still required.
- Runtime cert identity maps to `runtime_id`; unknown CN/identity maps to `unknown_runtime`.
- Cert chain validation uses configured CA; expiration uses standard TLS validation.
- Registry/cert mismatch should reject before worker/capability registration.
- Private key files must be operator-owned and mode-restricted where possible; docs must state expected permissions.

### SSH backend posture
- Kernel shells out to system `ssh`; do not add an in-process SSH library or test-only libssh variant.
- OpenSSH owns:
  - `~/.ssh/config`.
  - ProxyJump/jump hosts.
  - ssh-agent/hardware key integration.
  - known_hosts verification.
  - ControlMaster/multiplexing.
- Kernel-side config uses singular `[[runtime]]`:
  ```toml
  [[runtime]]
  id = "build-host"
  backend = "ssh"
  host = "build@build.team.example"
  remote_cmd = ["tabula-runtime", "stdio", "--token-file", "/etc/tabula/runtime-token"]
  ssh_command = ["ssh"]
  ```
- Runtime API frames travel over SSH stdio via the same codec and same `Hello` token handshake.
- Automated service kernels must pre-populate `known_hosts`; do not rely on first-contact interactive prompts in service mode.
- SSH stderr is surfaced in kernel logs without tokens.

### Token lifecycle
- Token format: `rtk_<32 bytes base64url>`.
- Local managed runtime:
  - Kernel writes `$TABULA_HOME/run/runtime-token` mode `0600` under `run/` mode `0700` on each startup.
  - Old local token invalidates on restart.
- Remote token store:
  - Kernel persists hashed tokens in `$TABULA_HOME/state/runtime-tokens.json` with metadata: runtime_id, token_hash, tenant_scope/allowed tenants, created_at, expires_at, last_seen_at, revoked flag as needed.
  - Plaintext token appears only at issue time.
- Runtime token source:
  - `token_file` points to a mode-restricted file on the runtime host.
  - Runtime re-reads token file before reconnect attempts to support rotation.
- CLI primitives:
  - `tabula runtime token issue --runtime-id <id> ...` prints plaintext once; `--json` supported.
  - `tabula runtime token list --json` prints metadata only.
  - `tabula runtime token revoke --runtime-id <id>` revokes and kicks active connections within the acceptance window.
- Logs/status:
  - Never print plaintext token.
  - Prefix/hash snippets are acceptable only if documented and non-sensitive.

### Service-install security posture
- `scripts/install-service.sh` and `scripts/uninstall-service.sh` own launchd/systemd orchestration.
- Service unit invokes `tabula serve` directly. Do not add/use `tabula serve --foreground`; foreground is the only server mode.
- Go subcommands remain composable data primitives; scripts do polling and operator messages.
- Generic service script belongs in `tabula`; distro-specific wrapping belongs in `tabula-distrib`.
- Service units should set/propagate `TABULA_HOME` explicitly.
- Log locations:
  - launchd stdout/stderr to `$TABULA_HOME/logs/kernel.log` or documented platform path.
  - systemd via journald unless file logging is explicitly configured.
- System mode requires explicit privileges; scripts must not auto-sudo.
- Docs must include uninstall, log inspection, restart, and token rotation/revocation procedures.

### Tenant and worker security carry-through
- Remote backend acceptance does not authorize all tenants by itself.
- Kernel checks tenant `[tenant] allowed_runtimes/default_runtime` before Invoke.
- Runtime checks `[[kernel]].tenants` before worker spawn.
- Worker SDK refuses missing `TABULA_TENANT_ID`, `TABULA_TENANT_DIR`, or `TABULA_KERNEL_ID`.
- Worker/process boundary remains: kernel has no tool-execution `exec.Command`; runtime-side `os/exec` is confined to worker launch and SSH backend process responsibilities.

### Operator-doc evidence requirements
M6-05 docs/testbed must cover:
- Which backend to choose: local, WSS, SSH.
- WSS production TLS and mTLS setup.
- Token issue/list/revoke/rotation and `token_file` storage.
- mTLS certificate trust model and what it adds over token-only.
- SSH known_hosts, jump hosts, ssh-agent, and debugging with `ssh -v`.
- Service install/uninstall and logs.
- Troubleshooting canonical errors: `unauthorized`, `unknown_runtime`, `tenant_forbidden`, `runtime_unavailable`, `protocol_error`.
- Explicit future-work note only for Docker/k8s/firecracker; do not imply M6 ships them.

### SECURITY handoff checklist
SECURITY should collect artifacts under `memory-bank/security/artifacts/execute-remote-runtime-program/` for:
- Token file modes (`0600`) and run dir mode (`0700`).
- No plaintext token logs.
- Restart regeneration and token-file reread on reconnect.
- Hashed remote token persistence and revoke disconnect timing.
- Unauthorized handshake, unknown runtime/cert mismatch, `client_auth=request|require` behavior.
- SSH uses system `ssh` out of process.
- Service unit command line is `tabula serve` with explicit `TABULA_HOME`.
- Tenant whitelist rejection occurs before worker spawn.
- Kernel has no tool-execution `exec.Command` after M3-08.

Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 9
    extensibility: 8
    failure_isolation: 9
    constraint_fit: 10
    simplicity: 7
  ai_slop_flags: none
  verdict: PASS
  notes: The selected security posture layers token, optional mTLS, tenant whitelist, and worker-env checks while keeping transport-specific trust decisions out of kernel policy.
