# ADR-0029: Retire Codex and standardize on Claude Code

- Status: Accepted
- Date: 2026-08-12
- Deciders: Cocola maintainers
- Supersedes: ADR-0022 runtime catalog decision

## Context

Cocola added Codex as a second built-in Agent Runtime together with an OpenAI Responses provider and gateway path.
The product no longer intends to support multiple Agent Runtimes, and there are no Codex conversations or Responses
provider configurations that require migration. Keeping the unused path would continue to expand the Sandbox image,
model configuration surface, protocol routing, tests, and release verification without user value.

## Decision

Cocola supports one built-in Agent Runtime: `claude-code`. Agent Runtime discovery remains a typed service contract,
but the registry contains exactly one entry and the Gateway validates that entry during startup. Runtime selection and
its environment-variable overrides are removed from the product surface.

Agent model providers use `anthropic-messages`. OpenAI-compatible embeddings remain supported only for Memory and are
not an Agent conversation protocol. The Admin API and UI no longer accept or display `openai_responses`; the LLM
Gateway no longer exposes `/v1/responses`; and the Sandbox Runtime no longer installs or dispatches the Codex CLI or
SDK.

The database migration refuses to proceed if Codex conversations or Responses providers/routes exist. This turns the
confirmed empty-history assumption into an explicit deployment invariant instead of silently deleting durable data.
Generic `runtime_id` fields remain in durable records so session ownership and audit data stay explicit; new writes are
validated against the single runtime.

## Alternatives Considered

- **Hide Responses only in Admin** — smallest UI change, but leaves a callable protocol, larger image, and unsupported
  runtime path that can drift or be accidentally re-enabled.
- **Keep Codex behind a feature flag** — avoids deletion but preserves almost all implementation and maintenance cost
  for a capability with no current product requirement.
- **Remove every `runtime_id` field** — produces a cosmetically simpler schema but requires a broad, risky rewrite of
  stable conversation, project, session, and audit contracts without improving the user experience.

## Consequences

- Model configuration has one Agent protocol and a separate embedding-only provider type.
- Sandbox images are smaller and contain one native Agent state directory.
- Gateway, Agent Runtime, and LLM Gateway have one deterministic conversation path.
- Reintroducing another runtime would require a new ADR and complete protocol, isolation, migration, and product design
  rather than reactivating hidden legacy branches.
