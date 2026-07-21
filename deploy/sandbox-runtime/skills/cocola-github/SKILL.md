---
name: cocola-github
description: Use GitHub from a Cocola Project that is already bound to a GitHub repository through the built-in gh CLI and run-scoped credential broker. Trigger for repository, pull request, issue, Actions, release, or GitHub API work. An unpublished local Project must first be published by the user in Cocola.
---

# Cocola GitHub

This platform Skill is adapted from `github/awesome-copilot`'s `gh-cli` Skill
at commit `e9a7805e2b1dbda5ad4d0cc9be1fc3ef6273e115`. Cocola supplies GitHub CLI
2.94.0 and replaces persistent authentication with a run-scoped, single-repo
broker.

## Rules

1. Work only with the repository already bound to the current Project. Check
   context with `git remote -v`, `git branch --show-current`, and `gh repo view
   --json nameWithOwner,defaultBranchRef`.
2. Never run `gh auth login`, write a token, install a credential helper, or
   print authentication environment variables. The `gh` wrapper acquires a
   short-lived token for each command and revokes it afterward.
3. Prefer structured output (`--json` with `--jq`) for reads. Use explicit
   titles and body files for PRs and Issues so shell quoting does not corrupt
   content.
4. In a GitHub-imported Project, push only the current `cocola/task-*` branch.
   A published local Project works directly on `main`, whose push requires an
   exact one-time user approval. Use
   `branch="$(git branch --show-current)"; cocola-sandbox github git -- push
   origin "HEAD:refs/heads/$branch"` instead of adding Git credentials to the
   repository.
5. Read operations, Task-branch push, PR/Issue creation and ordinary comments
   normally run automatically. Merge, default-branch or force push, deletion,
   repository settings, collaborators, deploy keys, webhooks, secrets,
   rulesets, and write-oriented `gh api` require an exact one-time Cocola
   approval. Do not alter the command after approval; request a new approval.
6. If the broker denies or times out, report that outcome. Do not bypass it
   with curl, a manually supplied token, SSH, or another repository.
7. For `gh api`, GraphQL, or a `gh` subcommand the wrapper cannot classify,
   call the guest CLI directly and declare the narrow repository permissions.
   This always requires approval, for example:
   `cocola-sandbox github gh --permissions actions=write -- workflow enable ci.yml`.
   Never request account, organization, billing, Gist, or another repository.

## Common commands

```bash
gh pr list --json number,title,state,headRefName
gh pr create --base main --head "$(git branch --show-current)" --title "..." --body-file /tmp/pr.md
gh issue list --json number,title,state,labels
gh run list --limit 20 --json databaseId,name,status,conclusion
gh release list --limit 20
branch="$(git branch --show-current)"
cocola-sandbox github git -- push origin "HEAD:refs/heads/$branch"
```

Use `gh <group> <command> --help` for the versioned command reference already
inside the image.
