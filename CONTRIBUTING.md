# Contributing to cocola

Thanks for helping improve cocola. Keep pull requests focused: one PR should address one logical concern and should not include unrelated cleanup.

## Before you start

- Search existing issues and pull requests.
- Open an issue before large features or architectural changes.
- Never include credentials, private URLs, customer data, or unredacted logs.
- Security vulnerabilities must follow [SECURITY.md](./SECURITY.md), not a public issue.

## Development environment

The repository uses:

- Go 1.24 for the workspace and Go 1.25 for `apps/sandbox-manager`
- Python 3.11 with [uv](https://docs.astral.sh/uv/)
- Node.js 22 with pnpm 9
- Docker and Docker Compose v2 for local infrastructure

Install dependencies with:

```sh
pnpm install --frozen-lockfile
make py-install
```

See [README.md](./README.md) for the development stack and service layout.

## Validate your change

Run the smallest relevant checks while developing, then run the full checks before opening a pull request:

```sh
make format-check
make lint
make test
node --test apps/web/lib/*.test.mjs
```

Changes to `apps/sandbox-manager` require:

```sh
cd apps/sandbox-manager
GOWORK=off go vet ./...
GOWORK=off go test ./...
```

## Change records and commits

Before committing a code change, add a Markdown change record under `docs/archive/` following the format documented in [AGENTS.md](./AGENTS.md).

Use a Conventional Commit title such as:

```text
feat(web): add project switcher
fix(gateway): persist interrupted runs
docs: clarify production setup
```

Do not skip repository hooks with `--no-verify`.

## Pull requests

A pull request should explain:

- what changed and why;
- how the change was tested;
- compatibility, deployment, migration, and security impact;
- linked issues for non-trivial work.

Maintainers may ask for unrelated changes to be split into separate pull requests.
