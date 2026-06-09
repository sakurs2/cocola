# cocola - root Makefile

.DEFAULT_GOAL := help

ROOT := $(shell pwd)
GO_APPS := gateway sandbox-manager admin-api
PY_APPS := agent-runtime llm-gateway

# -------------------------------------------------------------------- meta
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# -------------------------------------------------------------------- dev infra
.PHONY: dev-up dev-down dev-logs dev-ps
dev-up: ## Start local infra (PostgreSQL + Redis + MinIO)
	docker compose -f deploy/docker-compose/docker-compose.dev.yml up -d

dev-down: ## Stop local infra
	docker compose -f deploy/docker-compose/docker-compose.dev.yml down

dev-logs: ## Tail local infra logs
	docker compose -f deploy/docker-compose/docker-compose.dev.yml logs -f

dev-ps: ## Show local infra status
	docker compose -f deploy/docker-compose/docker-compose.dev.yml ps

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
	@cd packages/go-common && go mod tidy

go-build: ## Build all Go services
	@for a in $(GO_APPS); do (cd apps/$$a && go build -o ../../bin/$$a ./cmd/$$a); done

go-test: ## Run all Go tests
	@for a in $(GO_APPS); do (cd apps/$$a && go test ./...); done
	@cd packages/go-common && go test ./...

go-lint: ## Run golangci-lint
	golangci-lint run ./...

# -------------------------------------------------------------------- python
.PHONY: py-install py-test py-lint py-format
py-install: ## Install Python deps (uv sync)
	@for a in $(PY_APPS); do (cd apps/$$a && uv sync); done
	@cd packages/py-common && uv sync

py-test: ## Run Python tests
	@for a in $(PY_APPS); do (cd apps/$$a && uv run pytest); done

py-lint: ## Lint Python code (ruff)
	ruff check apps/agent-runtime apps/llm-gateway packages/py-common

py-format: ## Format Python code (ruff format)
	ruff format apps/agent-runtime apps/llm-gateway packages/py-common

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

# -------------------------------------------------------------------- sandbox (M1)
# The sandbox-manager Go build is run inside a Linux golang container. The
# corporate-managed macOS host blocks both the native TLS verifier and writing
# ".gitmodules" files, so a host-native `go build` cannot resolve modules.
# scripts/sandbox-build.sh encapsulates the working recipe.
.PHONY: sandbox-build sandbox-run sandbox-e2e sandbox-m2-e2e
sandbox-build: ## Build sandbox-manager + sandbox-cli (containerized)
	scripts/sandbox-build.sh

sandbox-run: sandbox-build ## Run sandbox-manager locally (Docker provider)
	COCOLA_SANDBOX_PROVIDER=docker ./bin/sandbox-manager

sandbox-e2e: ## Full M1 smoke test: Go CLI + Python runtime demos
	scripts/sandbox-e2e.sh

sandbox-m2-e2e: ## M2 acceptance: 50-session concurrency bench (needs Redis)
	scripts/sandbox-m2-e2e.sh

# -------------------------------------------------------------------- aggregate
.PHONY: install test lint clean
install: go-tidy py-install web-install ## Install all deps

test: go-test py-test ## Run all tests

lint: go-lint py-lint web-lint proto-lint ## Run all linters

clean: ## Remove build artifacts
	rm -rf bin/ apps/web/.next apps/web/out
	find . -type d -name __pycache__ -prune -exec rm -rf {} +
	find . -type d -name .pytest_cache -prune -exec rm -rf {} +
