.PHONY: fmt fmt-check clippy build test test-short integration-test clean dev dev-down dev-reset dev-logs dev-db go-test go-test-short

CARGO := cargo
CONTRACTS_DIR := packages/contracts
WASM_TARGET := wasm32-unknown-unknown

fmt:
	cd $(CONTRACTS_DIR) && $(CARGO) fmt --all

fmt-check:
	cd $(CONTRACTS_DIR) && $(CARGO) fmt --all -- --check

clippy:
	cd $(CONTRACTS_DIR) && $(CARGO) clippy --all-targets --all-features -- -D warnings

build:
	cd $(CONTRACTS_DIR) && $(CARGO) build --target $(WASM_TARGET) --release

test:
	cd $(CONTRACTS_DIR) && $(CARGO) test --all

integration-test:
	cd $(CONTRACTS_DIR) && $(CARGO) test --all --lib

go-test:
	cd apps/api && go test -race -timeout 120s ./...

go-test-short:
	cd apps/api && go test -race -short -timeout 60s ./...

clean:
	cd $(CONTRACTS_DIR) && $(CARGO) clean

# Docker Compose — local development

dev: ## Start all services with Docker Compose (migrations auto-apply)
	docker compose up --build

dev-down: ## Stop all services
	docker compose down

dev-reset: ## Destructive reset of database volumes and restart (use only for full rebuilds)
	docker compose down -v && docker compose up --build

dev-logs: ## Tail logs for all services
	docker compose logs -f

dev-db: ## Open a psql shell in the dev database
	docker compose exec postgres psql -U nester nester_dev
