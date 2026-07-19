# ADR-0024: Versioned Sandbox Runtime contract and operator profiles

- Status: Accepted
- Date: 2026-07-19
- Deciders: cocola maintainers

## Context

Cocola initially treated the sandbox image as a collection of preinstalled
tools. Direct Docker startup ran the image command, while OpenSandbox replaced
that command with provider-generated shell that independently launched Code
Server and slept forever. Workspace paths, optional services, resource defaults
and health state therefore had no single contract. Adding more sandbox
capabilities on those parallel lifecycle paths would make recovery and failure
handling increasingly provider-specific.

Phase 1 needs a stable foundation for later browser automation, artifacts and
an optional Sandbox MCP surface. It explicitly does not add Jupyter, a visual
desktop, per-Sandbox observe APIs, HTML publication, browser automation, or MCP.

## Decision

- The image contains a schema-versioned Runtime Manifest describing workspace
  paths, supported profiles, profile defaults and resident service metadata.
- Direct Docker and OpenSandbox converge on one
  `/opt/cocola/runtime-entrypoint.sh`. Providers may prepare mounts and links,
  but do not implement service lifecycle.
- `supervisord` owns optional resident processes. Code Server is supervised and
  may fail independently while the Sandbox remains available for Agent Exec.
  Supervisor configuration and launchers remain root-owned; Code Server always
  drops to the fixed non-root `cocola` identity before executing its binary.
- `/workspace`, `/workspace/outputs`, `/workspace/downloads`, `/session/runtime`
  and `/cache` form the stable guest path contract. Browser state is reserved at
  `/session/runtime/browser`; `/cache` stays ephemeral.
- `COCOLA_SANDBOX_PROFILE` is operator-owned and accepts `coding` or `minimal`.
  The default `coding` profile enables Code Server and uses a `1000m/2048Mi`
  resource floor. `minimal` disables Code Server and uses `500m/512Mi`.
  Explicit Sandbox resources and documented operator overrides take precedence.
- Sandbox Manager removes profile and service-policy keys supplied by Agent
  callers, validates operator values at startup and injects the effective
  policy into every new Sandbox. Profiles are not conversation or database
  settings.
- A separate guest CLI, `cocola-sandbox`, exposes versioned human/JSON commands
  for runtime information, service status and workspace information. Cocola's
  existing host CLI remains responsible for installation and operations.

## Alternatives Considered

- **Keep provider-specific startup scripts** — smaller immediate diff, but each
  optional capability would need duplicated lifecycle and readiness semantics.
- **Use one shell loop per service** — avoids a process manager dependency, but
  provides no uniform status interface, bounded retries or process-group stop.
- **Make profiles conversation-selectable now** — offers flexibility, but adds
  product/configuration surface before there is evidence that users need it and
  lets untrusted requests change platform cost policy.
- **Expose the guest contract only through MCP** — convenient for some Agents,
  but couples basic diagnostics to model/tool configuration. A deterministic CLI
  works for Agents, humans, tests and a later thin MCP adapter.

## Consequences

- **Positive** — every provider uses the same runtime lifecycle and workspace
  contract; optional-service failures become inspectable and isolated.
- **Positive** — future browser, artifact and MCP phases can extend a versioned
  manifest and guest CLI instead of adding provider-specific commands.
- **Negative** — the image adds `supervisor`, a manifest and a small guest CLI;
  changing this contract requires publishing a new runtime image.
- **Negative** — profile changes apply only to newly created Sandboxes and still
  require operators to size their nodes for the coding default.
- **Followup** — add on-demand browser automation, safe artifact/HTML preview,
  then an optional MCP adapter backed by the guest CLI contract.
