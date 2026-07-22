---
name: cocola-project-workspace
description: Use whenever the current Agent working directory is /workspace/project or the user asks to create, scaffold, initialize, restructure, build, or modify a Cocola Project. Keep a normal single-project application at the existing Git root; create nested applications or packages only when the user explicitly requests a monorepo or nested layout.
---

# Cocola Project Workspace

`/workspace/project` is the existing Git worktree for a Cocola Project. Keeping a
single application at that root lets Git, Preview, Code Server, deployment tools,
and future Agent turns agree on where the project lives.

## Establish the layout

1. Confirm the current worktree before changing files:

   ```bash
   pwd
   git rev-parse --show-toplevel
   git status --short
   ```

   For a Project run, both the working directory and Git top level should resolve
   to `/workspace/project` (Git may display its canonical `/session/...` path).

2. Treat the Cocola Project name, package name, and remote repository name as
   metadata. They do not require a same-named child directory in the worktree.

3. Keep platform-owned files outside the repository under `/workspace/outputs`,
   `/workspace/uploads`, and `/workspace/downloads`.

## Scaffold a single project

- Initialize the current directory instead of passing a new project-directory
  name. Use the framework's current-directory form, for example:

  ```bash
  npx create-next-app@latest .
  npm create vite@latest .
  ```

- Set names in package manifests or framework configuration after scaffolding;
  do not use an extra directory merely to obtain a desired package name.
- Inspect existing files before scaffolding and preserve `.git` and Cocola's Git
  marker. If a generator cannot target a non-empty Git worktree, stage its output
  in a temporary directory outside `/workspace/project`, check for conflicts, and
  copy only the generated contents into the worktree root. Do not replace `.git`.

## Nested layouts

Create `apps/<name>`, `packages/<name>`, or another nested application directory
only when the user explicitly asks for a monorepo, multi-package repository, or
nested project. Existing repositories that already use a nested layout retain
their structure; do not flatten them automatically.

Before finishing, run `git status --short` from the Git root and verify that a
normal single-project manifest such as `package.json`, `pyproject.toml`, or
`go.mod` is at the repository root when the selected framework uses one.
