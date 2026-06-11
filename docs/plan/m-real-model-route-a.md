# Plan: connect the real LLM model end-to-end (Route A)

- Status: Proposed (awaiting verification)
- Date: 2026-06-11
- Goal: a genuine (non-echo) agent turn streams back from `POST /v1/chat`,
  driven by the real Anthropic-compatible endpoint the user configured in the
  repo-root `.env` (`COCOLA_LLM_PROVIDER=anthropic` + `COCOLA_ANTHROPIC_*`).

## Why Route A (not Route B)

The containerised `agent-runtime` image is `python:3.11-slim` -- it has the
Python `claude-agent-sdk` but NO Node / no `claude` CLI, so Route B
(`ClaudeAgentSDKProvider`, which spawns the CLI on the agent-runtime host)
cannot run a real model in-container. ADR-0009's Route A is the path: the whole
Claude Code brain runs inside the user's sandbox, which uses the
`cocola/sandbox-runtime:dev` image (Node 22 + `claude` 2.1.170 + the stdio shim).

## Data path (real model)

```
web -> gateway -> agent-runtime (Route A router)
                   |  acquire(session) -> sandbox-manager (DooD) ->
                   |      creates a cocola/sandbox-runtime container
                   |      with ANTHROPIC_* env injected at creation
                   +- exec_stream(SHIM_ENTRYPOINT) into that sandbox
                          shim -> claude CLI -> ANTHROPIC_BASE_URL
                                 -> llm-gateway /v1/messages
                                 -> real Anthropic endpoint (.env creds)
```

## The one missing seam

`server.py` already calls `binder.acquire(session_id, user_id)`, and the
image+env chain (binder -> `SandboxClient` -> proto `SandboxSpec{image,env}` ->
docker provider) is fully implemented. What is missing: nobody supplies the
Route A sandbox image (default is `alpine:3.20`, no CLI) nor the model
credentials to inject. Fix it at the composition root, not in `server.py`:

1. `SandboxManagerBinder(addr, *, default_image="", default_env=None)` -- when a
   caller passes an empty image / env, fall back to these defaults (merged so an
   explicit per-call value still wins). Pure additive; existing callers/tests
   unaffected.
2. `__main__._build_binder()` reads the provisioning config and passes it in:
   - `COCOLA_SANDBOX_IMAGE` -> the Route A brain image.
   - `COCOLA_SANDBOX_LLM_BASE_URL` -> injected as `ANTHROPIC_BASE_URL`.
   - `COCOLA_SANDBOX_LLM_TOKEN`    -> injected as `ANTHROPIC_AUTH_TOKEN`.
   - `COCOLA_SANDBOX_MODEL_ALIAS`  -> injected as `ANTHROPIC_MODEL` and
     `ANTHROPIC_SMALL_FAST_MODEL` (so the CLI's main + fast model both resolve
     to a known gateway alias; the registry 404s unknown aliases).
   Credentials enter the sandbox ENV at creation, never the prompt channel
   (ADR-0009 sec.2).

## Networking

Sandboxes are created by sandbox-manager via DooD on the host Docker daemon,
so they land on the host default bridge, not the compose `cocola` network. They
reach the gateway through the host-published port: `ANTHROPIC_BASE_URL =
http://host.docker.internal:8081` (host 8081 -> llm-gateway:8080). Docker Desktop
provides `host.docker.internal` to all containers; on a bare Linux engine the
sandbox would need `extra_hosts` (out of scope here).

## Compose changes (`docker-compose.full.yml`)

- llm-gateway: pass the real provider via compose substitution from the
  repo-root `.env` -- `COCOLA_LLM_PROVIDER`, `COCOLA_ANTHROPIC_BASE_URL`,
  `COCOLA_ANTHROPIC_API_KEY`, `COCOLA_ANTHROPIC_MODEL`. Deliberately NOT the
  whole `.env` (that carries `COCOLA_AUTH_SECRET`, which would switch auth on and
  break the dev token flow). Auth stays OFF for this verification.
- agent-runtime: `COCOLA_AGENT_ROUTE=A`, `COCOLA_SANDBOX_IMAGE=
  cocola/sandbox-runtime:dev`, `COCOLA_SANDBOX_LLM_BASE_URL=
  http://host.docker.internal:8081`, `COCOLA_SANDBOX_LLM_TOKEN=cocola-local`,
  `COCOLA_SANDBOX_MODEL_ALIAS=cocola-default`.
- Run with `docker compose --env-file ../../.env -f docker-compose.full.yml up`.

## Acceptance

- Unit: `agent-runtime` tests stay green (binder default-image/env is additive).
- E2E: `POST /v1/chat` (with `session_id`) streams a `sandbox` event, then real
  model `text` (NOT the echo provider's canned reply), then `done`.
- `docker logs` on the user sandbox shows the shim -> claude CLI run; llm-gateway
  `/v1/usage` records a real token count.

## Out of scope

Egress allowlist enforcement, hibernate/resume, Postgres persistence, prod auth,
K8s provider. Bare-Linux `host.docker.internal` wiring.
