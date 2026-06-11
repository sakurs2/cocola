# Build for cocola agent-runtime (Python, managed by uv).
#
# Single-stage on purpose: the service depends on two FIRST-PARTY packages
# wired as EDITABLE path sources in pyproject ([tool.uv.sources]):
#   cocola-common -> ../../packages/py-common
#   cocola-proto  -> ../../packages/proto/gen/python
# Editable installs point the venv at the on-disk source, so that source must
# remain present at runtime. Keeping build+runtime in one stage avoids fragile
# venv relocation. Build context MUST be the repo root.
#
#   docker build -f apps/agent-runtime/Dockerfile -t cocola/agent-runtime:dev .

FROM python:3.11-slim

# uv: fast, reproducible installer/resolver (repo standard for Python apps).
RUN pip install --no-cache-dir uv

WORKDIR /app
# Preserve the relative layout the path sources expect.
COPY apps/agent-runtime          ./apps/agent-runtime
COPY packages/py-common          ./packages/py-common
COPY packages/proto/gen/python   ./packages/proto/gen/python

WORKDIR /app/apps/agent-runtime
# Resolve + install into a project-local .venv from the locked manifest.
RUN uv sync --no-dev --frozen

EXPOSE 50061
# uv run executes inside the synced .venv without an explicit activate step.
ENTRYPOINT ["uv", "run", "python", "-m", "cocola_agent_runtime"]
