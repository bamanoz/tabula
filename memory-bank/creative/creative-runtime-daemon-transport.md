# Creative Phase: Runtime Daemon Transport

## 1. PROBLEM DEFINITION
- What needs to be designed: the M2/M3 daemon/transport architecture that extracts plugin and skill execution from the kernel while preserving the accepted no-legacy cutover gates.
- Constraints:
  - Runtime daemon is always a separate OS process, including local mode.
  - Runtime always dials the kernel; the kernel listens.
  - Kernel-side stdio plugin transport is physically deleted in M2-07 with no fallback.
  - Kernel-side `process_manager.go` skill execution is temporary only through M2 and is physically deleted in M3-08.
  - Runtime ↔ worker stdio NDJSON is the chosen worker boundary and is not the legacy path being deleted.
  - Local auth uses unix socket permissions plus bearer-token handshake; remote transports reuse the same Runtime API contract later.
- Success criteria:
  - BUILD can implement local daemon, unix transport, token handshake, reconnect/reload semantics, and deletion gates without reintroducing embedded execution.
  - Testbed can exercise real installed layout after M2-07 instead of test-only embedded paths.
  - Runtime and backend lifecycle behavior is clear under timeout, cancellation, reload, disconnect, and restart.
- Non-functional requirements:
  - Fail fast on disconnect and preserve warm workers across reconnect unless `Reload` is issued.
  - Keep kernel generic: backends produce `RuntimeConn`; kernel does not host/sandbox tools.
  - Security posture must be strong enough for SECURITY to audit token files, logs, and process boundaries.

## 2. OPTIONS

### Option A: Listener kernel + dial-out runtime with shared codec
- Description: Kernel opens a local unix socket at `$TABULA_HOME/run/runtime.sock`; local backend starts `tabula-runtime` as a managed child; runtime reads `token_file`, dials the kernel, sends `Hello`, then speaks the shared Runtime API codec. WSS/SSH later reuse the same typed frames.
- Architecture: Backend layer owns child/dial/reconnect; `internal/runtime/transport` owns frame codec; `cmd/tabula-runtime` owns worker supervision.
- Advantages:
  - Matches ADR Q1/Q2 exactly.
  - Same direction for local and remote attach flows.
  - Clear auth first frame and reload semantics.
  - Enables no-kernel-stdio deletion without an embedded fallback.
- Disadvantages:
  - Local mode adds a managed child and a socket even on one host.
  - Startup ordering requires readiness polling in `bootstrap.sh`/testbed.
- Risk factors:
  - Token refresh/re-read on reconnect can be missed if the runtime caches tokens too aggressively.

### Option B: Kernel dials runtime-owned socket in local mode
- Description: Managed child runtime opens its own socket and kernel dials into it.
- Architecture: Direction differs between local and attach/WSS remote modes.
- Advantages:
  - Runtime looks more like a conventional local server.
  - Kernel may not need to expose a listener before child start.
- Disadvantages:
  - Violates the accepted “runtime always dials” decision.
  - Two direction models complicate N:M runtime attachments and reconnect behavior.
  - Harder to reuse remote auth/listener posture.
- Risk factors:
  - Local-only assumptions creep into the Runtime API lifecycle.

### Option C: Keep embedded/stdin fallback for tests and recovery
- Description: Extract daemon but leave a kernel fallback that still spawns plugins/skills if runtime is unavailable.
- Architecture: Kernel keeps production or test-only tool execution path alongside Runtime API.
- Advantages:
  - Easier short-term debugging if daemon extraction is broken.
  - Unit tests can avoid spawning a real runtime.
- Disadvantages:
  - Directly violates AGENTS no-legacy and ADR §10.
  - Preserves the exact coupling the program is removing.
  - Makes testbed less representative and weakens SECURITY process-boundary checks.
- Risk factors:
  - “Test-only” path becomes production behavior by accident.

## 3. ANALYSIS

| Criterion | Weight | Option A | Option B | Option C |
|-----------|--------|----------|----------|----------|
| Complexity | 3 | 4 | 3 | 2 |
| Performance | 2 | 5 | 5 | 5 |
| Maintainability | 5 | 5 | 3 | 1 |
| Scalability | 4 | 5 | 3 | 1 |
| Security | 5 | 5 | 3 | 1 |
| **Weighted Total** | | **92** | 61 | 37 |

## 4. DECISION
**Selected: Option A — listener kernel + dial-out runtime with shared codec.**

Justification: This is the accepted ADR topology and the only option that keeps local, attach, WSS, and SSH behavior converging on one `RuntimeConn` contract. It also gives the no-legacy deletion gates a crisp line: after M2-07 the kernel can only invoke plugins via Runtime API; after M3-08 all skills follow the same route.

Trade-offs accepted:
- Local startup has one extra process and socket readiness step.
- M2-07 is a real atomic risk because there is no embedded fallback.

## 5. IMPLEMENTATION GUIDELINES

### Local unix transport shape
- Kernel creates `$TABULA_HOME/run/` with mode `0700`.
- Kernel listens on `$TABULA_HOME/run/runtime.sock`; socket mode should be `0600` where platform permits.
- Kernel generates a local token on every `tabula serve` startup and writes `$TABULA_HOME/run/runtime-token` mode `0600`.
- `tabula-runtime` reads `token_file = "${TABULA_HOME}/run/runtime-token"`, dials the unix socket, and sends `Hello{runtime_id:"local", token,...}`.
- Token generation failure is fatal to `tabula serve`; do not silently disable auth.
- The local managed child path is a backend concern only. The kernel must not spawn tool workers.

### Token handshake and refresh semantics
- `Hello` is the first authenticated Runtime API frame.
- Good token → `HelloAck{ok:true}` and connection registration.
- Wrong/missing token → `HelloAck{ok:false,error.code:"unauthorized"}` then close.
- Runtime must log auth failure clearly without printing token material.
- Runtime reconnect loop must re-read `token_file` before each redial so restart regeneration and operator rotation work.
- Kernel logs must not include plaintext token; SECURITY will check logs for leakage.

### Managed child lifecycle
- `tabula serve` starts `tabula-runtime` as a managed child only for configured local backend(s).
- A single Ctrl+C/termination of `tabula serve` should tear down the managed child.
- Runtime exits non-zero for local missing token file or unauthorized ack.
- `tabula status --json` is the readiness/read-model primitive consumed by shell scripts; do not add Go prompts or orchestration.
- Installer/testbed must execute installed `tabula` and `tabula-runtime` binaries, not package internals.

### Reconnect, disconnect, and reload
- Runtime reconnect: exponential backoff 1s, 2s, 4s, capped at 60s, indefinite until explicit shutdown.
- On reconnect: repeat `Hello`, token validation, and capability listing.
- Kernel fail-fast: pending invokes on a dropped connection complete with `runtime_unavailable`, retryable true.
- In-flight calls from before disconnect are not recovered.
- Warm workers survive disconnect because they are owned by the runtime daemon, not the connection.
- Reload is separate from reconnect:
  - Kernel sends `Reload` after manifest/config/generation changes.
  - Runtime evicts selected warm workers and reloads config/manifests.
  - Reload is the only deliberate stale-worker cleanup path; do not piggyback it on every reconnect.

### Worker stdio boundary
- Runtime ↔ worker uses stdio NDJSON always.
- This stdio path is internal to the runtime daemon and remains valid after M2/M3.
- Worker identity is keyed by `(kernel_id, tenant_id, target_id)`.
- Plugins are warm workers by default; skills are cold workers by default once M3 lands.
- Runtime-side `os/exec` is allowed for worker/backend launch responsibilities; kernel-side tool execution is not.

### Deletion gates and sequencing
- M2-07 plugin deletion gate:
  - Must wait for `M2-01..06`, `M2-04`, and `M2-04b` merged and cited by SHA/PR URL.
  - Delete `internal/kernel/plugin/runtime.go` stdio plugin transport or equivalent kernel-side spawn path.
  - No `embedded`, no `stdio`, no “test-only” fallback backend in kernel.
  - `process_manager.go` remains explicitly temporary only for skills through M2.
- M3-08 skill deletion gate:
  - Must wait for runtime skill manifests/harnesses/cold pool and `M3-07` skill routing through `RuntimeConn.Invoke`.
  - Delete `internal/kernel/process_manager.go`.
  - Add or strengthen guard forbidding tool-execution `exec.Command` callsites under `internal/kernel/`.
- M5/M6 must not reintroduce kernel execution paths for `fs`, `exec`, SSH, service install, or tests.

### BUILD handoff checklist
- Add focused tests for codec, unix socket auth, token permission modes, reconnect token reread, reload, and disconnect fail-fast.
- Add installed-layout testbed at M2-08 using `bootstrap.sh` + `tabula status --json`.
- Ensure runtime smoke tests execute the installed runtime binary.
- Preserve kernel generic boundary: distro policy and bundle SDK migration live in their respective repos.

Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: 9
    extensibility: 9
    failure_isolation: 8
    constraint_fit: 10
    simplicity: 8
  ai_slop_flags: none
  verdict: PASS
  notes: The selected transport design maps directly to ADR Q1/Q2/Q10 and gives BUILD explicit deletion gates that prevent legacy execution paths from surviving.
