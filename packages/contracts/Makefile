.PHONY: build build-adapters test integration-test fmt clippy clean help deploy-testnet

# Yield source adapters (#812). Each external protocol gets one small adapter
# implementing nester_common::adapters::YieldAdapter. Adding a third protocol
# means adding its package here — no vault changes.
ADAPTER_PKGS := adapter-lending-contract adapter-pool-contract

help:
	@echo "Nester Soroban Workspace Commands"
	@echo "=================================="
	@echo "make build            - Build all contracts to WASM"
	@echo "make build-adapters   - Build only the yield source adapters"
	@echo "make test             - Run all unit tests"
	@echo "make integration-test - Run multi-contract integration tests only"
	@echo "make fmt              - Format code with cargo fmt"
	@echo "make clippy           - Run clippy linter (must pass clean)"
	@echo "make clean            - Remove build artifacts"
	@echo "make deploy-testnet   - Deploy contracts to Stellar testnet"

build:
	@echo "Building all Soroban contracts to WASM..."
	@echo "  Building vault_token (required by vault)..."
	@cargo build --release --target wasm32-unknown-unknown -p vault-token || exit 1
	@for contract in contracts/*/; do \
		if [ -f "$$contract/Cargo.toml" ]; then \
			pkg_name=$$(grep '^name = ' "$$contract/Cargo.toml" | sed 's/name = "\(.*\)"/\1/'); \
			if [ "$$pkg_name" != "vault-token" ]; then \
				echo "  Building $$pkg_name..."; \
				cargo build --release --target wasm32-unknown-unknown -p "$$pkg_name" || exit 1; \
			fi \
		fi \
	done
	@echo "✓ Build successful - all contracts compiled to WASM"

build-adapters:
	@echo "Building yield source adapters to WASM..."
	@for pkg in $(ADAPTER_PKGS); do \
		echo "  Building $$pkg..."; \
		cargo build --release --target wasm32-unknown-unknown -p "$$pkg" || exit 1; \
	done
	@echo "✓ Adapters compiled to WASM"

test:
	@echo "Running all tests..."
	@cargo test --lib
	@echo "✓ All tests passed"

integration-test:
	@echo "Running multi-contract integration tests..."
	@cargo test -p nester-integration-tests --lib
	@echo "✓ Integration tests passed"

fmt:
	@echo "Formatting code..."
	@cargo fmt --all
	@echo "✓ Code formatted"

clippy:
	@echo "Running clippy linter..."
	@cargo clippy --all --all-targets --all-features -- -D warnings
	@echo "✓ Clippy passed with zero warnings"

clean:
	@echo "Cleaning build artifacts..."
	@cargo clean
	@echo "✓ Clean complete"

deploy-testnet:
	@bash scripts/deploy-testnet.sh
