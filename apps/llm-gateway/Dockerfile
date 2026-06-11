# Build for cocola llm-gateway (Python/FastAPI, managed by uv).
#
# Single-stage for the same reason as agent-runtime: cocola-common is an
# EDITABLE path source (../../packages/py-common) that must exist at runtime.
# Build context MUST be the repo root.
#
#   docker build -f apps/llm-gateway/Dockerfile -t cocola/llm-gateway:dev .

FROM python:3.11-slim

RUN pip install --no-cache-dir uv

WORKDIR /app
COPY apps/llm-gateway    ./apps/llm-gateway
COPY packages/py-common  ./packages/py-common

WORKDIR /app/apps/llm-gateway
RUN uv sync --no-dev --frozen

EXPOSE 8080
ENTRYPOINT ["uv", "run", "python", "-m", "cocola_llm_gateway"]
