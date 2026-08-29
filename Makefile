.PHONY: fmt fmt-check clippy build test test-short integration-test clean dev dev-external dev-down dev-reset dev-logs dev-db go-test go-test-short

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
#
# By default, all services bind to 127.0.0.1 (loopback only) for security.
# This prevents accidental exposure on shared networks.
#
# For multi-machine setups or mobile testing, opt in to external binding:
#   make dev-external
#
# See docker-compose.external.yml and docs/security/dev-setup.md.

dev: ## Start all services with Docker Compose (migrations auto-apply)
	docker compose up --build

dev-external: ## Start all services with external binding (0.0.0.0) — use only on trusted networks
	docker compose -f docker-compose.yml -f docker-compose.external.yml up --build

dev-down: ## Stop all services
	docker compose down

dev-reset: ## Destructive reset of database volumes and restart (use only for full rebuilds)
	docker compose down -v && docker compose up --build

dev-logs: ## Tail logs for all services
	docker compose logs -f

dev-db: ## Open a psql shell in the dev database
	docker compose exec postgres psql -U nester nester_dev

dev-seed: ## Apply scripts/seed.sql to the running dev database (re-runnable)
	docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U nester nester_dev < scripts/seed.sql

# The documented reset for #1122. Drops the schema, lets the API container
# re-apply every migration on start, then loads the fixture set — so a
# contributor whose database has drifted gets back to a known state without
# guessing which migration they are missing.
dev-db-reset: ## Recreate the dev schema, re-run migrations, and re-seed
	docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U nester nester_dev -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'
	docker compose restart api
	@echo "Waiting for migrations to apply..."
	@until docker compose exec -T postgres psql -tA -U nester nester_dev -c "SELECT to_regclass('public.users')" | grep -q users; do sleep 1; done
	$(MAKE) dev-seed
