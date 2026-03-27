.PHONY: fmt fmt-check clippy build test integration-test clean deploy-testnet deploy-all

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

clean:
	cd $(CONTRACTS_DIR) && $(CARGO) clean

deploy-testnet:
	cd $(CONTRACTS_DIR) && bash scripts/deploy.sh

deploy-all: build deploy-testnet
