# AGENTS.md — Cocola Multi-Agent Collaboration Guidelines

> This file applies to every AI agent contributing to Cocola. **Before starting any coding task, read this file and review `docs/archive/` for recent project history.**

## 1. Project Overview

Cocola is a self-hosted enterprise Agent platform with Go and Python backends, a Next.js frontend, and the Claude Code Agent SDK. See [`README.md`](./README.md) for the product, technology stack, and repository structure, and [`docs/adr/`](./docs/adr/) for architecture decisions.

Engineering conventions:

- Use **uv** for all Python projects.
- Prefer mature open-source solutions over building equivalent infrastructure from scratch.
- Never bypass Git hooks when committing (`--no-verify` is prohibited), and never amend commits created by others.
- Start the local development stack with `make dev`. Stop it through the script's graceful shutdown path so that no service processes or occupied ports remain.

## 2. Product and Solution Design Principles (Mandatory)

### 2.1 Start from the User's Goal

- Before adding a feature, identify its target user, the problem it solves, and the shortest primary path through which the user completes that goal. Do not design solely around data models, APIs, or implementation convenience.
- Prefer entry points, concepts, and interactions that users already understand. Do not add pages, steps, settings, or implementation concepts without a clear user benefit.
- Provide clear defaults for common cases and place expert or infrequent options behind secondary entry points. Ordinary users must not need to understand the system architecture before completing a task.
- Name features, buttons, and states in terms of outcomes users understand. Use the same term for an action at its entry point, confirmation, execution state, and result message.
- A feature should be understandable from its structure and essential copy. If it requires lengthy explanatory text, revisit the information architecture and interaction flow first.

### 2.2 Control Product and System Complexity

- Apply Occam's razor: among solutions that satisfy the user goal, reliability, and security constraints, choose the one with fewer concepts, states, and dependencies.
- Evaluate both benefits and costs for every technical proposal. At minimum, cover implementation effort, system complexity, runtime reliability, operational burden, usability, failure modes, and long-term maintenance cost.
- Do not introduce multiple implementations, generic abstractions, permanent services, or synchronization mechanisms for a hypothetical future need. Add complexity only when a concrete requirement provides value now.
- When a feature's marginal value does not justify a serious impact on complexity, stability, operations, or usability, explicitly recommend against it and propose a simpler alternative instead of implementing it mechanically.

## 3. Frontend and Interaction Guidelines (Mandatory)

- Prefer the existing design system and free HeroUI components. Reuse the project's component wrappers, theme tokens, button hierarchy, and status styles. Add a custom component only when HeroUI and existing components cannot meet the requirement.
- Confirmation actions must use a Modal or Dialog centered in the browser window. Drawers and sidebars are for navigation, detail inspection, and longer editing flows; never use them as the final confirmation for deletion, merge, overwrite, refresh recovery, or similar actions.
- Keep layouts compact, clearly structured, and efficient with available space. Avoid large areas of meaningless whitespace, unnecessarily tall cards, loose spacing, and decorative containers that do not communicate structure.
- Keep interface copy concise. The primary surface should contain only the titles, labels, and guidance required to complete the current task. Put secondary explanations in a Tooltip, Popover, help entry, or documentation instead of stacking prose on the page.
- Controls in the same family must use consistent sizing, corner radius, icons, alignment, colors, and interaction states. Do not mix visibly different button styles or densities within one action group.
- Truncated branch names, paths, IDs, and similar values must reveal the complete value on hover or keyboard focus. Identifiers that users frequently reuse should also provide a copy action.
- Inspect UI changes on the real page and at the target viewport. At minimum, verify long content, empty, loading, error, and narrow-screen states; component tests alone are not sufficient evidence of visual correctness.

## 4. Backend and Implementation Quality (Mandatory)

### 4.1 Technical Design Requirements

- Technical designs must be simple, clearly bounded, reliable, and stable. Prefer extending existing domain abstractions and deterministic capabilities over implementing the same business rule in parallel.
- Design reviews must explain key tradeoffs and relevant failure handling, including timeouts, cancellation, retries, idempotency, concurrency, resource cleanup, degradation, and observability where applicable.
- Identify the authoritative data source. A temporary workspace, cache, frontend state, or single process's memory must not implicitly become the authoritative source for durable business data.
- Express cross-service capabilities through stable, typed contracts. When changing Protobuf definitions, database schemas, or public interfaces, evaluate compatibility, generated code, migrations, and every caller together.

### 4.2 Prohibited Implementation Patterns

- **No trick logic:** correctness must not depend on hidden side effects, accidental call order, magic delays, brittle string parsing, or special branches understood only by the author.
- **No obviously inefficient logic:** avoid unbounded loops, busy polling, N+1 requests, repeated scans, duplicate remote calls, and data processing that repeatedly traverses multiple layers when it can be aggregated directly. Explain complexity or provide measurements for performance-sensitive paths.
- **No logic that cannot be supported long term:** do not implement production capabilities through temporary processes, machine-local paths, manual repair, untraceable state, or bypasses around existing architecture boundaries. A temporary compatibility path must document its exit condition and cleanup plan.
- **Do not conceal errors:** never swallow exceptions, present failure as success, or use unconditional fallbacks to hide inconsistent data. Preserve a diagnosable cause and translate it into user-understandable information at the appropriate boundary.
- Cover success, failure, and critical boundary paths. Changes involving concurrency or external side effects must also cover timeout, cancellation, retry, idempotency, and resource cleanup. Every bug fix must add a regression test that would have caught the original issue.

## 5. Agent Operations and Git Authorization Boundaries (Mandatory)

- An agent may inspect, modify, and verify code only within the task authorized by the user. Never opportunistically modify, clean up, or commit unrelated user changes.
- Run `git commit` only when the user explicitly asks for a commit. Permission to commit does not include permission to push; run `git push` only when the user explicitly asks for a push.
- Creating or merging a Pull Request, publishing a release, deploying an environment, and any other operation that changes remote state each require explicit user authorization for that specific action. Never infer these permissions from a request to implement or commit code.
- Before committing, inspect `git status`, the working-tree diff, and the staged diff. Stage only files required for the current logical change, and confirm that the commit contains no secrets, credentials, private keys, `.env` contents, debug artifacts, or unrelated changes.
- Keep every commit focused on one logical purpose and use Conventional Commits. The message must accurately describe the actual diff. Every code commit must include a `docs/archive/` change record that satisfies Section 7.
- Before committing, run the smallest sufficient tests and formatting checks for the change. Do not skip applicable verification because a change is described as documentation-only or small.
- Never bypass hooks with `--no-verify`, amend another contributor's commit, force-push, or rewrite the history of a shared branch. If a commit attempt fails, fix the issue and make a new commit attempt without overwriting an existing commit.
- Before deleting a branch, tag, remote resource, or performing another destructive Git operation, resolve the exact target and obtain explicit user authorization.

## 6. Formatting and Style Checks (Mandatory)

All code must be formatted before commit. The repository uses [pre-commit](https://pre-commit.com/) with [`.pre-commit-config.yaml`](./.pre-commit-config.yaml). All hooks are local and do not rely on remote repositories, avoiding failures caused by corporate TLS proxies.

### 6.1 Tools by Language and File Type

| Language / files        | Formatter                                      | Linter                                      | Configuration                                    |
| ----------------------- | ---------------------------------------------- | ------------------------------------------- | ------------------------------------------------ |
| Python                  | `ruff format`                                  | `ruff check --fix`                          | `ruff.toml`, `packages/py-common/pyproject.toml` |
| Go                      | `gofmt -w -s`                                  | `golangci-lint` (CI/Make, not every commit) | `.golangci.yml`                                  |
| TS/JS/JSON/CSS/MD/YAML  | `prettier`                                     | `next lint` (web, CI/Make)                  | `.prettierrc.json`, `.prettierignore`            |
| Protobuf                | `buf format`                                   | `buf lint` (Make)                           | `packages/proto/buf.yaml`                        |
| General text formatting | Trim trailing whitespace and add final newline | —                                           | `.editorconfig`                                  |

The pre-commit hooks perform only **fast, automatically fixable** formatting and lightweight linting. Authoritative slower checks such as `golangci-lint`, `next lint`, and `buf lint` run through `make lint` and CI. If `buf` or `prettier` is unavailable, its hook exits successfully instead of blocking the commit; once installed, it runs automatically.

### 6.2 Initial Setup for Every Contributor or Environment

```bash
pip install pre-commit          # or: uv tool install pre-commit
make precommit-install          # install .git/hooks/pre-commit
pnpm install                    # make prettier available
pre-commit run --all-files      # format the existing repository once
```

### 6.3 Common Commands

- `make format`: format all supported languages (Go, Python, and web).
- `make format-check`: check formatting without writing; used by CI.
- `make lint`: run all authoritative linters (`golangci-lint`, Ruff, Next.js lint, and Buf lint).
- When a commit is blocked, hooks usually have already fixed the files. Stage those fixes and run `git commit` again.

> Never use `--no-verify` to bypass hooks. To skip an individual hook when explicitly justified, use `SKIP=<hook-id> git commit`.

## 7. Change Records (Mandatory)

To support multi-agent collaboration, **create a Markdown change record under `docs/archive/` before every code commit**. This is a normal version-controlled directory and must be included in the same commit.

### 7.1 File Naming

Use `<change-type>-<short-description>.md`.

- **Change type** follows Conventional Commits: `feat`, `fix`, `chore`, `refactor`, `docs`, `test`, `perf`, `build`, or `ci`.
- **Short description** uses hyphens and must uniquely identify the change.
- Examples: `feat-graceful-teardown.md`, `fix-claude-config-isolation-503.md`.
- If the name already exists, add a date prefix, for example `20260610-fix-xxx.md`.

### 7.2 Required Content

Every record must contain at least:

1. **Change time** — accurate to the minute and including the time zone.
2. **Reason for the change**
   - For a bug fix, describe the symptom, trigger, and root cause.
   - For a feature, describe the user need or business context.
3. **What changed** — identify the files, behavior, and important design tradeoffs.

### 7.3 Template

```markdown
# <type>: <one-sentence title>

- Change time: 2026-06-10 14:50 (+08:00)
- Related commit or PR: <commit hash or PR URL, optional>

## Reason

(For a bug fix, document the symptom, trigger, and root cause. For a feature, document the user need or business context.)

## Changes

- path/to/file_a: what changed
- path/to/file_b: what changed
- Key tradeoffs or notes
```

## 8. Reading Project History

`docs/archive/` is the project's human-readable change log and is committed with the repository. **Before starting a new task, review the relevant recent records** to understand:

- what changed recently and why;
- established design decisions and previously discovered pitfalls, so they are not repeated or accidentally reversed.

Read it together with `git log` and `docs/adr/` for a complete view of the project's evolution.
