# ADR-0026: Workspace Artifacts and isolated static HTML preview

- Status: Accepted
- Date: 2026-07-20
- Deciders: cocola maintainers

## Context

Cocola already uploaded changed files from `/workspace/outputs` into the object
store and rendered them in a conversation-owned side panel. That behavior was
not part of the versioned Sandbox Runtime contract, so Agents only discovered
it through a short system prompt. The output scan also followed symbolic links.

HTML was fetched and placed in a sandboxed `srcdoc` iframe, but the Artifact
download endpoint still served user-controlled bytes inline on Cocola's
authenticated origin. The iframe allowed scripts, forms, popups and navigation,
and HTML could reference external resources. Those paths were broader than the
static, user-visible deliverable preview required for this phase.

This phase must reuse the existing object-store and conversation authorization
model. It does not add live-site deployment, an automatic preview tab, an
Artifact HTTP server inside the Sandbox, or a Sandbox MCP adapter.

## Decision

- The Runtime Manifest declares a required `artifacts` workspace-output
  capability for both `coding` and `minimal` Profiles. Its stable output root is
  `/workspace/outputs`.
- `cocola-sandbox artifact status/list` exposes the effective contract and a
  bounded, machine-readable inventory. Inventory traversal never follows
  symbolic-link directories and reports only regular files.
- Agent Runtime snapshots and publishes only changed regular files. Symbolic
  links and non-regular entries are ignored. Publication still happens after
  the turn through the existing MinIO-backed, conversation-owned Artifact
  event path.
- The image includes a version-aligned `cocola-sandbox-artifacts` Skill. It
  teaches Agents to keep temporary files outside `outputs`, verify the final
  inventory, and make HTML deliverables a single self-contained document.
- Artifact byte responses always use attachment disposition, `nosniff`,
  private/no-store caching, same-origin resource policy and a deny-by-default
  CSP. User-controlled HTML is never served as executable inline content on the
  authenticated Cocola origin.
- The Web UI fetches image and PDF bytes before creating a local object URL.
  HTML is parsed as an inert document; scripts, event handlers, remote/relative
  URL attributes and embedded browsing contexts are removed. The sanitized
  document receives a deny-by-default CSP and renders in an opaque-origin iframe
  without Sandbox permissions. Rendered HTML is limited to 2 MiB to bound
  client-side parsing; source mode continues to show the original bytes.
- Static HTML preview supports inline CSS and `data:` media. JavaScript does not
  execute. Agents use the separate on-demand Browser over temporary loopback
  HTTP when interactive verification is needed.

## Alternatives Considered

- **Keep active `srcdoc` scripts in an opaque origin** — protects Cocola cookies,
  but scripts can still attempt network requests and navigate their own frame;
  it is unnecessary for a downloadable static Artifact.
- **Serve previews from a dedicated untrusted origin** — a strong future option
  for active applications, but requires separate DNS, routing, authentication
  and asset-bundle semantics. The current static preview can be isolated without
  introducing that operational surface.
- **Publish a directory as a multi-file website** — preserves relative assets,
  but requires a bundle manifest, path routing and content rewriting. A
  self-contained HTML file gives deterministic preview and download semantics.
- **Add an in-Sandbox Artifact server** — duplicates the existing object-store
  publication path and creates a resident port with recovery and authorization
  requirements.

## Consequences

- **Positive** — Artifact creation is discoverable through the same versioned
  guest contract and built-in Skill as other Sandbox capabilities.
- **Positive** — direct downloads and previews no longer execute active Artifact
  content on Cocola's authenticated origin, while image, PDF, Markdown, code and
  static HTML previews remain available.
- **Positive** — no new daemon, port, polling loop or provider-specific startup
  path is introduced.
- **Negative** — HTML that depends on JavaScript, remote assets or multiple files
  must be made self-contained/static for Artifact preview, or tested through the
  Browser/Preview Proxy flow.
- **Followup** — add an optional Sandbox MCP adapter over the versioned guest CLI
  only after the CLI contracts have stabilized; live-site automatic tabs remain
  a separate product phase.
