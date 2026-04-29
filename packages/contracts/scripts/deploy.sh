#!/bin/bash

# Exit on error
set -e

NETWORK="testnet"
RPC_URL="https://soroban-testnet.stellar.org:443"
NETWORK_PASSPHRASE="Test SDF Network ; September 2015"
SOURCE_ACCOUNT="default" # Assumes 'default' identity is configured in stellar-cli

# Directory where WASM files are located
WASM_DIR="../target/wasm32-unknown-unknown/release"
CONFIG_FILE="../contracts.toml"

echo "Deploying contracts to $NETWORK..."

# Function to deploy and return contract ID
deploy_contract() {
    local wasm_path=$1
    echo "Deploying $(basename "$wasm_path")..."
    
    # Install WASM and get hash
    local wasm_hash=$(stellar contract install \
        --network "$NETWORK" \
        --source "$SOURCE_ACCOUNT" \
        --wasm "$wasm_path")
    
    # Deploy contract and get ID
    local contract_id=$(stellar contract deploy \
        --network "$NETWORK" \
        --source "$SOURCE_ACCOUNT" \
        --wasm-hash "$wasm_hash")
    
    echo "$contract_id"
}

# 1. Deploy Vault Token
VAULT_TOKEN_WASM="$WASM_DIR/vault_token.wasm"
VAULT_TOKEN_ID=$(deploy_contract "$VAULT_TOKEN_WASM")
echo "Vault Token ID: $VAULT_TOKEN_ID"

# 2. Deploy Yield Registry
YIELD_REGISTRY_WASM="$WASM_DIR/yield_registry.wasm"
YIELD_REGISTRY_ID=$(deploy_contract "$YIELD_REGISTRY_WASM")
echo "Yield Registry ID: $YIELD_REGISTRY_ID"

# 3. Deploy Vault
VAULT_WASM="$WASM_DIR/vault_contract.wasm"
VAULT_ID=$(deploy_contract "$VAULT_WASM")
echo "Vault ID: $VAULT_ID"

# Update contracts.toml (Simple sed for demonstration, better to use a TOML parser)
# Note: This is a fragile way to update TOML, but works for the requirements.
sed -i "s/vault = .*/vault = \"$VAULT_ID\"/" "$CONFIG_FILE"
sed -i "s/vault_token = .*/vault_token = \"$VAULT_TOKEN_ID\"/" "$CONFIG_FILE"
sed -i "s/yield_registry = .*/yield_registry = \"$YIELD_REGISTRY_ID\"/" "$CONFIG_FILE"

echo "Success! Contract IDs saved to $CONFIG_FILE"
