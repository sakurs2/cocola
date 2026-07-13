# Architecture Decision Records

This directory captures the **why** behind structural decisions in cocola. Each
ADR is immutable once accepted; supersede it with a new ADR rather than editing
in place.

## Why ADRs

Code shows _what_ and _how_. ADRs preserve _why_ — invaluable when:

- new contributors ask "why isn't this Service X?"
- you revisit a decision 6 months later
- a tech option's tradeoffs shift

## Filing a new ADR

1. Copy `template.md` to `NNNN-short-kebab-title.md` (next sequence number).
2. Fill in: Context → Decision → Consequences.
3. Open a PR. Discussion happens on the PR, not by editing the ADR.
4. On merge, status flips to `Accepted`.

## Index

| #    | Title                                                                                                 | Status             |
| ---- | ----------------------------------------------------------------------------------------------------- | ------------------ |
| 0001 | Tech stack: Go + Python hybrid, Next.js frontend                                                      | Accepted           |
| 0002 | Sandbox provider abstraction                                                                          | Accepted           |
| 0003 | Redis-backed session↔sandbox binding with lease + two-stage GC                                        | Accepted           |
| 0004 | LLM gateway as an Anthropic-compatible proxy in front of the Claude Agent SDK, with a metering ledger | Accepted           |
| 0005 | Identity as a cocola-signed token (the SDK's API key) + period-windowed token quota                   | Accepted           |
| 0006 | Admin-api control plane (Go) + Skill-Market catalog, with token revocation and dynamic quota          | Accepted           |
| 0007 | Gateway BFF + agent-runtime gRPC split                                                                | Accepted           |
| 0008 | Persistence layering, lifecycle, and sandbox backend (K8s + gVisor)                                   | Accepted           |
| 0009 | Run the Claude Code agent runtime inside each user's sandbox (Route A)                                | Accepted           |
| 0010 | Gateway tool-use passthrough (Anthropic rich-payload)                                                 | Accepted           |
| 0011 | Observability three pillars (RED metrics + OTel tracing) and load-testing baseline                    | Accepted           |
| 0012 | Warm-pool pre-warm strategy under the PVC/bind-mount volume model (amends ADR-0008 §3)                | Superseded by 0016 |
| 0013 | OpenSandbox as a pluggable SandboxProvider backend (not a sandbox-layer replacement)                  | Accepted           |
| 0014 | OpenSandbox as the primary sandbox backend; retire k8s provider (docker kept as fallback)             | Accepted           |
| 0015 | On-demand cold-start allocation as default; warm pool kept as optional (OpenSandbox-only)             | Amended by 0019    |
| 0016 | Remove the warm pool capability entirely (supersedes 0012; amends 0015)                               | Superseded by 0019 |
| 0017 | Chat attachment storage layering + sandbox delivery (push model; P0 inline, OSS/pull as TODO)         | Accepted           |
| 0018 | Conversation audit summaries and product-level trace spans                                            | Accepted           |
| 0019 | Single-Gateway reconnectable chat runs                                                                | Accepted           |
| 0020 | Configuration ownership and reload boundaries                                                         | Accepted           |
| 0021 | Standalone CLI and release-asset bootstrap installer                                                  | Accepted           |
