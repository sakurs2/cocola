# Multi-stage build for cocola web (Next.js, pnpm workspace).
#
# Build context MUST be the repo root: web depends on @cocola/ts-common
# (workspace:*), so the pnpm workspace manifest + the ts-common package must be
# in the context.
#
#   docker build -f apps/web/Dockerfile -t cocola/web:dev .

# ---- deps + build stage --------------------------------------------------
FROM node:22-slim AS build
RUN corepack enable
WORKDIR /repo

# Workspace manifests first for better layer caching.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json .npmrc ./
COPY patches ./patches
COPY apps/web/package.json          ./apps/web/package.json
COPY packages/ts-common/package.json ./packages/ts-common/package.json
COPY packages/ui-compat/package.json ./packages/ui-compat/package.json
RUN pnpm install --frozen-lockfile

# Then the sources and build.
COPY apps/web        ./apps/web
COPY packages/ts-common ./packages/ts-common
COPY packages/ui-compat ./packages/ui-compat
RUN pnpm --filter @cocola/web build

# ---- runtime stage -------------------------------------------------------
FROM node:22-slim AS runtime
WORKDIR /repo
ENV NODE_ENV=production
# Bring over the fully built workspace (node_modules + .next + sources).
COPY --from=build /repo ./
WORKDIR /repo/apps/web
EXPOSE 3000
ENV COCOLA_WEB_HOST=0.0.0.0 PORT=3000
# Custom Node server (server.mjs) instead of `next start`: it wraps Next but adds
# an `upgrade` handler so the same-origin Preview Proxy can carry WebSockets
# (code-server editor + dev-server HMR). It reads COCOLA_WEB_HOST/PORT from the env,
# binding 0.0.0.0:3000, and uses the prebuilt .next (NODE_ENV=production -> dev
# off). See apps/web/server.mjs for the ADR/auth rationale.
ENTRYPOINT ["node", "server.mjs"]
