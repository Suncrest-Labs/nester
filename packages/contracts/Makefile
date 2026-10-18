.PHONY: build build-launch check-launch-size build-adapters test integration-test fmt clippy clean help deploy-testnet

# Yield source adapters (#812) that ship on mainnet per launch-manifest.toml (#1501).
ADAPTER_PKGS := adapter-blend-contract adapter-soroswap-contract

help:
	@echo "Nester Soroban Workspace Commands"
	@echo "=================================="
	@echo "make build             - Build launch-manifest contracts to WASM"
	@echo "make build-launch      - Build only contracts & adapters in launch-manifest.toml"
	@echo "make check-launch-size - Verify launch WASMs fit within manifest size budgets"
	@echo "make build-adapters    - Build only the yield source adapters"
	@echo "make test              - Run all unit tests"
	@echo "make integration-test  - Run multi-contract integration tests only"
	@echo "make fmt               - Format code with cargo fmt"
	@echo "make clippy            - Run clippy linter (must pass clean)"
	@echo "make clean             - Remove build artifacts"
	@echo "make deploy-testnet    - Deploy contracts to Stellar testnet"

build: build-launch

build-launch:
	@echo "Building mainnet launch contracts from launch-manifest.toml..."
	@for pkg in $$(bash scripts/check-launch-manifest.sh --list-packages); do \
		echo "  Building $$pkg..."; \
		cargo build --release --target wasm32-unknown-unknown -p "$$pkg" || exit 1; \
	done
	@echo "✓ Launch build successful - all manifest contracts compiled to WASM"

check-launch-size:
	@echo "Checking WASM size budgets against launch-manifest.toml..."
	@bash scripts/check-launch-manifest.sh --check-size target/wasm32-unknown-unknown/release
	@echo "✓ All launch WASMs are within their manifest size budget"

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
