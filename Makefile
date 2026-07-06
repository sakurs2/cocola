# cocola - root Makefile

.DEFAULT_GOAL := help

ROOT := $(shell pwd)
GO_APPS := gateway admin-api
SANDBOX_APP := sandbox-manager
PY_APPS := agent-runtime llm-gateway
DOCKER_COMPOSE := scripts/docker-compose.sh

# -------------------------------------------------------------------- meta
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# -------------------------------------------------------------------- dev infra
.PHONY: dev-up dev-down dev-logs dev-ps
dev-up: ## Start local infra (PostgreSQL + Redis + MinIO)
	$(DOCKER_COMPOSE) -f deploy/docker-compose/docker-compose.dev.yml up -d

dev-down: ## Stop local infra
	$(DOCKER_COMPOSE) -f deploy/docker-compose/docker-compose.dev.yml down

dev-logs: ## Tail local infra logs
	$(DOCKER_COMPOSE) -f deploy/docker-compose/docker-compose.dev.yml logs -f

dev-ps: ## Show local infra status
	$(DOCKER_COMPOSE) -f deploy/docker-compose/docker-compose.dev.yml ps

# -------------------------------------------------------------------- proto
.PHONY: proto-lint proto-gen proto-breaking proto-gen-py
proto-lint: ## Lint .proto files via buf
	cd packages/proto && buf lint

proto-gen: ## Generate stubs via buf
	cd packages/proto && buf generate

proto-breaking: ## Check breaking changes against main
	cd packages/proto && buf breaking --against '.git#branch=main'

proto-gen-py: ## Generate Python stubs (containerized; corporate-TLS safe)
	scripts/proto-gen-py.sh

# -------------------------------------------------------------------- go
.PHONY: go-tidy go-build go-test go-lint
go-tidy: ## go mod tidy for all Go modules
	@for a in $(GO_APPS); do (cd apps/$$a && go mod tidy); done
	@cd apps/$(SANDBOX_APP) && GOWORK=off go mod tidy
	@cd packages/go-common && go mod tidy

go-build: ## Build all Go services
	@for a in $(GO_APPS); do (cd apps/$$a && go build -o ../../bin/$$a ./cmd/$$a); done
	@cd apps/$(SANDBOX_APP) && GOWORK=off go build -o ../../bin/$(SANDBOX_APP) ./cmd/$(SANDBOX_APP)

go-test: ## Run all Go tests
	@for a in $(GO_APPS); do (cd apps/$$a && go test ./...); done
	@cd apps/$(SANDBOX_APP) && GOWORK=off go test ./...
	@cd packages/go-common && go test ./...

go-lint: ## Run golangci-lint
	golangci-lint run ./...

go-format: ## Format Go code (gofmt -w -s)
	@gofmt -w -s $$(git ls-files '*.go' | grep -v -E '(/gen/|\.pb\.go$$)')

go-format-check: ## Check gofmt formatting (lists offenders, non-zero if any)
	@out=$$(gofmt -l -s $$(git ls-files '*.go' | grep -v -E '(/gen/|\.pb\.go$$)')); \
		if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# -------------------------------------------------------------------- python
.PHONY: py-install py-test py-lint py-format
py-install: ## Install Python deps (uv sync)
	@for a in $(PY_APPS); do (cd apps/$$a && uv sync); done
	@cd packages/py-common && uv sync

py-test: ## Run Python tests
	@for a in $(PY_APPS); do (cd apps/$$a && uv run pytest); done

py-lint: ## Lint Python code (ruff)
	ruff check apps/agent-runtime apps/llm-gateway packages/py-common scripts

py-format: ## Format Python code (ruff format)
	ruff format apps/agent-runtime apps/llm-gateway packages/py-common scripts

# -------------------------------------------------------------------- frontend
.PHONY: web-install web-dev web-build web-lint
web-install: ## Install web deps
	cd apps/web && pnpm install

web-dev: ## Run Next.js dev server
	cd apps/web && pnpm dev

web-build: ## Build Next.js production bundle
	cd apps/web && pnpm build

web-lint: ## Lint web code
	cd apps/web && pnpm lint

web-format: ## Format web/ts/json/md/yaml with prettier
	node_modules/.bin/prettier --write --ignore-unknown "apps/web/**/*.{ts,tsx,js,jsx,css,json,md}" "packages/ts-common/**/*.ts"

web-format-check: ## Check prettier formatting (no write)
	node_modules/.bin/prettier --check --ignore-unknown "apps/web/**/*.{ts,tsx,js,jsx,css,json,md}" "packages/ts-common/**/*.ts"

# -------------------------------------------------------------------- sandbox / runtime
.PHONY: verify-opensandbox verify-opensandbox-full opensandbox-up opensandbox-down verify-opensandbox-k8s sandbox-runtime-verify sandbox-runtime-publish

# sandbox-manager is a standalone Go module deliberately kept OUT of go.work.
# So every sandbox-manager go command must run from inside the module with
# GOWORK=off. This target wraps the manual OpenSandbox provider verification
# harness (cmd/opensandbox-verify). Requires COCOLA_OPENSANDBOX_URL (and
# COCOLA_OPENSANDBOX_API_KEY if the server enables auth) in the environment.
# Extra flags pass through via ARGS=.
verify-opensandbox: ## Run the OpenSandbox provider e2e harness (needs COCOLA_OPENSANDBOX_URL)
	cd apps/sandbox-manager && GOWORK=off go run ./cmd/opensandbox-verify $(ARGS)

# One-stop e2e: stand up a real OpenSandbox server (the device under test) on
# :8090 via docker compose, wait for it to report healthy, then run the harness
# against it. The server is left running afterwards so you can re-run the harness
# or inspect it; tear it down with `make opensandbox-down`. Requires a running
# Docker daemon. Auth is off by default (no API key needed).
OPENSANDBOX_COMPOSE := deploy/docker-compose/docker-compose.opensandbox.yml
verify-opensandbox-full: opensandbox-up ## Deploy a local OpenSandbox server, then run the e2e harness
	COCOLA_OPENSANDBOX_URL=$${COCOLA_OPENSANDBOX_URL:-http://localhost:8090/v1} \
		$(MAKE) verify-opensandbox

opensandbox-up: ## Start a local OpenSandbox server on :8090 and wait for /health
	$(DOCKER_COMPOSE) -f $(OPENSANDBOX_COMPOSE) up -d
	@echo "waiting for OpenSandbox server /health on :8090 ..."
	@for i in $$(seq 1 60); do \
		if curl -fsS http://localhost:8090/health >/dev/null 2>&1; then \
			echo "OpenSandbox server is healthy"; exit 0; fi; \
		sleep 2; \
	done; \
	echo "ERROR: OpenSandbox server did not become healthy in time"; \
	$(DOCKER_COMPOSE) -f $(OPENSANDBOX_COMPOSE) logs --tail=50 opensandbox-server; \
	exit 1

opensandbox-down: ## Stop and remove the local OpenSandbox server
	$(DOCKER_COMPOSE) -f $(OPENSANDBOX_COMPOSE) down

verify-opensandbox-k8s: ## Verify OpenSandbox Kubernetes runtime with the configured sandbox image
	COCOLA_OPENSANDBOX_URL=$${COCOLA_OPENSANDBOX_URL:-http://127.0.0.1:8090/v1} \
		$(MAKE) verify-opensandbox ARGS="-image $${COCOLA_K8S_SANDBOX_IMAGE_REMOTE:-ghcr.io/sakurs2/cocola-sandbox-runtime:latest} -skip-pause $(ARGS)"

sandbox-runtime-verify: ## Build/self-check the sandbox runtime image
	scripts/sandbox-runtime-verify.sh

sandbox-runtime-publish: ## Publish the sandbox runtime image to GHCR
	scripts/sandbox-runtime-publish.sh

# -------------------------------------------------------------------- dev stack
# Local app stack. `make dev` is the DEFAULT debug mode: OpenSandbox Kubernetes
# runtime plus redis/postgres/minio run in containers; cocola-authored services
# run natively in the foreground for fast edit/restart loops. Formal/full
# Docker startup remains available as `make prod`.
.PHONY: dev prod
dev: ## Dev debug mode: cocola services NATIVE, sandbox runtime + infra containerized
	@echo "==> cocola mode: dev (local debug; native cocola services + containerized sandbox/infra)"
	@bash scripts/run-stack-dev.sh; status=$$?; \
		if [ "$$status" -eq 130 ] || [ "$$status" -eq 143 ]; then \
			echo "==> cocola dev stopped"; \
			exit 0; \
		fi; \
		exit "$$status"

prod: ## Prod mode: formal/full Docker startup (start.sh + docker-compose.full.yml)
	@echo "==> cocola mode: prod (full Docker stack)"
	bash scripts/start.sh

# -------------------------------------------------------------------- aggregate
.PHONY: install test lint format format-check precommit-install clean
install: go-tidy py-install web-install ## Install all deps

test: go-test py-test ## Run all tests

lint: go-lint py-lint web-lint proto-lint ## Run all linters

format: go-format py-format web-format ## Auto-format all languages (Go/Python/web)

format-check: go-format-check web-format-check ## Verify formatting without writing (CI)
	ruff format --check apps/agent-runtime apps/llm-gateway packages/py-common scripts
	ruff check apps/agent-runtime apps/llm-gateway packages/py-common scripts

precommit-install: ## Install the git pre-commit hook (pip/uv install pre-commit first)
	pre-commit install
	@echo "pre-commit hook installed. Run 'pre-commit run --all-files' to format everything."

clean: ## Remove build artifacts
	rm -rf bin/ apps/web/.next apps/web/out
	find . -type d -name __pycache__ -prune -exec rm -rf {} +
	find . -type d -name .pytest_cache -prune -exec rm -rf {} +
