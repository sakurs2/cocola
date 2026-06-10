# ADR-0002: SandboxProvider abstraction — no direct K8s/Docker coupling

- Status: Accepted
- Date: 2026-06-09
- Deciders: @wangjiahui

## Context

The Agent runtime must execute untrusted, model-generated commands (shell, file
IO, code) in an isolated sandbox. cocola targets multiple deployment shapes over
its lifetime:

1. **Local / single-host dev** — one machine with a Docker daemon. Zero cluster.
2. **Production multi-tenant** — Kubernetes scheduling many sandboxes, each
   wrapped in a gVisor (`runsc`) runtime for kernel isolation.
3. **Community / BYO backends** — hosted sandbox vendors (E2B, CubeSandbox) or a
   future Firecracker microVM provider.

Forces at play:

- The orchestration substrate (Docker Engine API vs. K8s API vs. a vendor REST
  API) is wildly different per backend and pulls in heavy, conflicting SDKs.
- M1 must ship a working loop _now_ without committing the codebase to K8s.
- We promised second-developers a stack where adding a backend is local and
  additive, not a cross-cutting refactor.

What we explicitly do **not** address here: the K8s+gVisor provider itself
(deferred), pause/resume snapshotting semantics beyond container freeze, and
network egress enforcement (the field exists in the spec but is a no-op in M1).

## Decision

Define a single Go interface, `provider.SandboxProvider`, as the **only** seam
between the service layer and any concrete backend. The gRPC `SandboxService`
contract in `packages/proto/cocola/sandbox/v1` maps 1:1 onto this interface, so
the wire API and the internal abstraction never drift.

```
Create / Exec(stream) / WriteFile / ReadFile / Pause / Resume / Destroy / Health
```

Key rules, enforced by package layout:

- **The service layer (`internal/server`) and `main.go` depend only on the
  `provider` package**, never on `docker`, `client-go`, or any vendor SDK.
- Concrete backends live under `internal/provider/<name>/` and either are
  selected by an explicit `case` in the `newProvider` factory or self-register
  via `provider.Register(name, impl)` in their `init()`.
- The backend is chosen at startup from `COCOLA_SANDBOX_PROVIDER`
  (default `docker`). Nothing below the factory knows which backend is live.
- Adding a backend = one new package + (at most) one `case` line. No edits to
  the service, the proto, or the Agent runtime.

For M1 the only implementation is **DockerProvider** (per the agreed scope):
it uses the Docker Engine API to run an `alpine` container per sandbox, mounts
the three-tier directory model (`/data/userdata/<user>` RW, `/workspace/<session>`
RW, `/data/plugins` RO), streams exec output via `stdcopy` demux, and does file
IO through tar streams.

## Alternatives Considered

- **Depend directly on the Kubernetes API (client-go) everywhere.**
  Rejected: forces a cluster for local dev, bloats every binary with client-go,
  and bakes K8s assumptions (pods, PVCs, CRDs) into the service layer. It would
  also make the Docker-only M1 impossible without a throwaway shim.

- **Depend directly on the Docker SDK in the service layer (skip the interface).**
  Rejected: fastest path to M1, but every future backend (K8s, E2B) would be a
  cross-cutting rewrite of the service and the Agent runtime — the opposite of
  the project's "additive extension" goal.

- **Shell out to the `docker` / `kubectl` CLIs.**
  Rejected: brittle parsing, no streaming primitives, version-coupled, and hard
  to test. The native SDKs give typed errors and stream handles.

- **One gRPC service per backend.**
  Rejected: explodes the proto surface and pushes backend selection up to every
  caller. A single service + internal factory keeps the contract stable.

## Consequences

- **Positive** — Docker-only M1 ships immediately; K8s+gVisor lands later as a
  pure addition; the Agent runtime (Python) is fully decoupled, talking only
  gRPC. Backends are independently testable against the interface.
- **Positive** — the proto ↔ interface 1:1 mapping means a backend that compiles
  against `SandboxProvider` is automatically wire-compatible.
- **Negative** — the interface is a lowest-common-denominator: backend-specific
  features (e.g. gVisor syscall policy, K8s node affinity) must be expressed
  through generic fields (`Resources`, `Networking`, env) or future spec
  extensions, not bespoke methods.
- **Negative** — `Pause`/`Resume` semantics differ across backends (Docker
  freeze vs. K8s pod suspension vs. vendor snapshot); callers must treat them as
  best-effort hints, not guarantees.
- **Followups** — implement `K8sGVisorProvider`; enforce `Networking.EgressAllowlist`;
  define snapshot semantics for `Pause`/`Resume`; add a provider conformance
  test suite that runs the same scenarios against every registered backend.
