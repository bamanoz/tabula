# ADR 0022: Evolution Release Campaigns and External Recovery

- Status: Accepted
- Date: 2026-08-01
- Supersedes: ADR 0012 candidate-generation and installer-activation wording
- Superseded by: none

## Context

ADR 0012 established evolution as an optional behavioral bundle, but described
activation in terms of installer-owned immutable generations. ADR 0020 later
restricted the installer to crash-safe replacement of one installed distro
path, and ADR 0021 introduced generic bundle-delivered host services outside the
kernel and replaceable distro tree.

Evolution needs two different trust domains. A controller may plan, modify
approved source repositories, verify and review commits, assemble candidates,
and request activation. It cannot safely stop its own runtime, decide that its
candidate is healthy, or own the only rollback copy. Recovery authority must
continue running when the candidate kernel, runtime, Python environment,
plugins, or distro cannot start.

## Decision

The independent `evolution` bundle contains:

- `change-control`, which ends at reviewed source commits and durable evidence;
- `evolution`, a tenant plugin that owns campaign policy, candidate assembly,
  approval, and activation requests;
- `evolution-supervisor`, a generic host-service component that owns activation,
  host-side health observation, known-good retention, rollback, and interrupted
  transaction recovery.

Kernel and installer gain no evolution concepts.

### Source and state ownership

Approved authoring repositories are explicit configuration. Development may use
existing local checkouts. Managed production should use mirrors and isolated
worktrees outside `TABULA_HOME`. Installed distro, package, plugin, runtime, and
binary trees are never authoring sources.

Controller state is tenant-local under
`tenants/<tenant>/state/plugins/evolution`. Sealed candidate artifacts live
under `$TABULA_HOME/evolution/candidates/<candidate-id>`. Supervisor journals,
receipts, known-good material, runtime releases, and protected configuration
live under `$TABULA_HOME/host-services/evolution-supervisor/state`.

### Campaign and authority

Campaign scope and activation authority are separate monotonic ceilings:

- scope: `light < advanced < pro`;
- activation authority: `manual < automatic`.

A campaign may lower either configured ceiling and may never raise it. `light`
can record analysis and proposals only. `advanced` may consume reviewed commits
for extension and distro targets. `pro` additionally permits runtime, kernel,
installer, and evolution targets.

Campaign state records objective, actor, budget, base release, source
transactions, candidate, evidence, activation request, supervisor receipt, and
outcome. `absorbed` is valid only after an exact-candidate supervisor health
receipt is committed.

### Sealed candidate

A candidate is a directory containing `candidate.json` and `payload/`. Its ID is
the SHA-256 of a canonical unsigned manifest that binds:

- parent release and approved target ID;
- exact reviewed source commits and their evidence digests;
- bundle and distro lock digests;
- protocol compatibility;
- every payload path, mode, size, and SHA-256;
- restart class and required external health profile;
- verification and review evidence digests.

Any manifest or payload change creates a different candidate ID and invalidates
approval and prior activation requests.

Candidate manifests select only a supervisor-configured target ID. They cannot
supply stop, start, install, rollback, or health commands. Those mechanics live
in protected supervisor target configuration.

### Supervisor protocol and transaction

Controller submits a request over a mode-0600 local Unix socket. Supervisor
persists the validated request before acknowledging it. Durable request,
journal, and receipt files make the socket an ingress mechanism, not the source
of truth. Requests bind candidate ID and expected active release; stale requests
fail closed.

Supervisor serializes activation and journals these phases:

1. `accepted`: request and sealed candidate validated;
2. `prepared`: candidate and complete known-good target retained;
3. `quiesced`: current runtime or target stopped;
4. `switched`: candidate applied or active runtime reference changed;
5. `started`: candidate runtime started;
6. `observing`: host-side probes and crash-free window running;
7. `committed`: active and known-good descriptors advanced, success receipt
   written;
8. `rolling_back`: candidate stopped and previous full target restored;
9. `rolled_back`: retained known-good restarted and verified, failure receipt
   written.

On restart, a non-terminal journal resumes rollback unless it contains enough
unambiguous data to prove no switch occurred. Corrupt or ambiguous state fails
closed and retains known-good material.

### Target adapters

Supervisor owns adapters, not installer:

- directory adapter provides deterministic test and generic file-tree switching;
- distro adapter invokes ordinary crash-safe `tabula-install distro install` on
  a self-contained candidate source and restores retained source through the
  same installer path;
- runtime adapter installs complete release directories beside known-good,
  atomically switches a supervisor-owned active release reference, and invokes
  protected service stop/start commands that launch through that reference.

Health probes execute outside candidate code and can include process liveness,
file receipts, fixed host commands, protocol compatibility, expected release ID,
tenant readiness, capability presence, and installed smoke tools. Candidate code
cannot write supervisor receipts or mark itself healthy.

### Recovery and self-update boundary

Supervisor retains at least the previous complete target and release descriptor.
Timeout, crash, protocol mismatch, smoke failure, or boot-loop threshold triggers
rollback and verification of known-good. Failure to restore known-good is a
terminal recovery error recorded separately from candidate failure.

A campaign may not activate a candidate that changes the supervisor controlling
that activation. Supervisor updates require a separate owner-approved trusted
transaction, retained previous supervisor executable, and external service
handoff. This lifecycle is outside normal campaigns.

Same-user execution protects primarily against accidental and candidate-runtime
failure, not a malicious process with the same account. Production hardening
uses isolated builder/reviewer workers and a separately protected supervisor OS
identity and state directory.

## Consequences

- Evolution remains optional and bundle-owned.
- Reviewed commits, candidate assembly, activation authority, and health verdicts
  have distinct owners.
- Installer remains a staged replacement primitive with no history or candidate
  API.
- Runtime binaries are never overwritten in place during activation.
- Full rollback remains possible when candidate kernel and plugin code cannot
  start.
- Distros must explicitly configure supervisor targets and managed launch paths.
- Linux persistent host-service support still depends on a future systemd
  adapter under ADR 0021.
