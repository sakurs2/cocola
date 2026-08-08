# ADR-0027: Sandbox command supervision as a Runtime contract

- Status: Proposed
- Date: 2026-08-08
- Deciders: cocola maintainers

## Context

OpenSandbox's command stream is a transport, not a process-lifecycle boundary.
Closing or cancelling that stream does not reliably terminate the guest process
or its descendants. Cocola therefore introduced a shell wrapper that starts each
execution with `setsid`, records its process-group id in `/tmp`, and issues TERM
then KILL from a second control command.

That wrapper fixed broad process-name killing, but it also exposed lifecycle
semantics through a generated shell string. In particular, Linux `setsid` forks
when its caller is already a process-group leader. Without `--wait`, the
observable parent returned immediately, OpenSandbox reported success, and the
real Agent continued detached. The Gateway then persisted a successful
assistant message with no answer.

The immediate compatibility fix is `setsid --wait` plus a Gateway invariant
that rejects an empty successful Agent response. The long-term design must make
command completion, output, cancellation, timeout and exit status independent
of provider-specific shell behavior. It must continue to work under
OpenSandbox/gVisor, where writable cgroup delegation is not always available.

## Decision

- The versioned Sandbox Runtime will provide a small root-owned guest executable
  at `/opt/cocola/bin/cocola-exec`. It is the lifecycle authority for foreground
  executions; provider adapters only transport a structured execution request
  and relay its byte streams.
- A run request contains a schema version, unguessable execution id, argv, cwd,
  environment, timeout and the operator-selected guest identity. The request is
  delivered as structured data rather than interpolated into a command shell.
- `cocola-exec run` forks the command into a new session/process group, writes an
  atomic root-owned record under `/run/cocola/executions`, continuously drains
  stdout and stderr, waits for the real child, and emits one structured terminal
  result containing exit code, signal and cancellation/timeout classification.
  The record includes the process start identity needed to reject PID reuse.
- `cocola-exec cancel --id` is idempotent. It validates the recorded process
  identity, sends TERM to the execution group, waits for a bounded grace period,
  then sends KILL and does not report completion until the target is gone.
- Foreground execution is the default contract. A workload that must survive its
  initiating tool call uses a separate explicit background/service contract;
  double-forking is not treated as an implicit persistence API.
- When the provider delegates a writable cgroup v2 subtree, the supervisor also
  assigns each execution its own cgroup and uses `cgroup.kill` as the authoritative
  descendant cleanup. The process group remains the required portable baseline
  for providers and gVisor configurations without cgroup delegation.
- The Sandbox Runtime selfcheck and provider conformance suite exercise real
  Linux behavior: caller already a process-group leader, delayed success,
  non-zero exit propagation, TERM handling, KILL escalation, PID-reuse defense,
  concurrent executions and bounded output draining.
- Gateway finalization independently rejects a nominally successful run that has
  no user-visible text, tool call, artifact, plan, question or structured result.
  It persists `EMPTY_AGENT_RESPONSE` instead of an empty success message.
- Until every supported Runtime image advertises the supervisor capability,
  provider adapters retain the corrected `setsid --wait` compatibility path.
  The shell marker implementation is removed only after the minimum Runtime
  contract version has advanced.

## Alternatives Considered

- **Keep extending the generated shell wrapper** — small changes are easy, but
  quoting, process identity, signal forwarding and platform-specific utility
  behavior remain distributed between Go and shell and are difficult to test as
  one state machine.
- **Treat the OpenSandbox stream as cancellation** — simpler, but transport
  cancellation has already proven not to terminate the guest process tree.
- **Find and kill descendants by command name or `ps` scans** — does not survive
  reparenting reliably and can kill unrelated workspace services.
- **Require per-execution cgroups immediately** — strongest tree boundary, but
  current OpenSandbox/gVisor deployments do not universally delegate a writable
  cgroup subtree to the sandbox.
- **Add a resident general-purpose execution daemon immediately** — enables
  reconnectable output and persistent jobs, but adds a new privileged service
  before the foreground execution protocol is stable. The dedicated executable
  leaves room for that evolution without requiring it for the first migration.

## Consequences

- **Positive** — one guest component owns process lifecycle and terminal-state
  semantics across providers; provider code no longer constructs lifecycle
  shell programs.
- **Positive** — cancellation becomes scoped, idempotent and testable against
  real Linux behavior, while cgroup support can strengthen the same contract.
- **Positive** — empty model/runtime results can no longer appear as successful
  blank answers even if a lower layer violates the execution contract.
- **Negative** — the Sandbox image gains a small privileged executable and a
  versioned execution protocol that must be maintained and security-reviewed.
- **Negative** — adopting the supervisor requires a Runtime image rollout and a
  compatibility window in sandbox-manager.
- **Followup** — implement the guest executable and conformance harness; advertise
  its capability in the Runtime Manifest; migrate OpenSandbox and direct-Docker
  adapters; add explicit persistent/background jobs; then remove the shell
  marker path after the minimum supported Runtime version advances.
