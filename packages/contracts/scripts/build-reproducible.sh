#!/usr/bin/env bash
# =============================================================================
# Reproducible Soroban WASM Build Script (Issue #1493)
# =============================================================================
# Builds all Nester Soroban contracts deterministically using either:
#   1. A digest-pinned Rust container image (default when Docker is used), or
#   2. The pinned local toolchain (`packages/contracts/rust-toolchain.toml`)
#      when invoked with `--local` (used inside the container or in CI).
#
# Outputs:
#   - Optimized WASM binaries in `target/wasm32-unknown-unknown/release/`
#   - Deterministic `SHA256SUMS` manifest and `SHA256SUMS.md` release notes
#     snippet in `target/wasm32-unknown-unknown/release/`.
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTRACTS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$CONTRACTS_DIR/../.." && pwd)"

# Digest-pinned Debian Bookworm Rust 1.91.0 image for bit-for-bit reproducible builds
PINNED_RUST_IMAGE="docker.io/library/rust:1.91.0-slim-bookworm@sha256:643f91b3a6c8e0f1074c48be2d1f44e7a56e4d23c2f5829098ca98a93f97d31e"
PINNED_RUST_CHANNEL="1.91.0"
WASM_TARGET="wasm32-unknown-unknown"

MODE="docker"
if [[ "${1:-}" == "--local" ]]; then
  MODE="local"
fi

if [[ "$MODE" == "docker" ]]; then
  if ! command -v docker >/dev/null 2>&1; then
    echo "ERROR: docker is required for containerized reproducible builds (or pass --local)." >&2
    exit 1
  fi

  echo "==> Running reproducible Soroban WASM build inside digest-pinned container:"
  echo "    Image: $PINNED_RUST_IMAGE"
  docker run --rm \
    -v "$REPO_ROOT:/workspace" \
    -w /workspace/packages/contracts \
    -e SOURCE_DATE_EPOCH=1700000000 \
    -e CARGO_INCREMENTAL=0 \
    -e LC_ALL=C \
    -e TZ=UTC \
    "$PINNED_RUST_IMAGE" \
    bash -c "rustup target add $WASM_TARGET && bash scripts/build-reproducible.sh --local"
  exit 0
fi

cd "$CONTRACTS_DIR"

# Verify compiler matches rust-toolchain.toml channel
ACTUAL_RUSTC="$(rustc --version)"
if [[ "$ACTUAL_RUSTC" != *"${PINNED_RUST_CHANNEL}"* ]]; then
  echo "ERROR: rustc version mismatch. Expected ${PINNED_RUST_CHANNEL}, got: ${ACTUAL_RUSTC}" >&2
  exit 1
fi

export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-1700000000}"
export CARGO_INCREMENTAL=0
export LC_ALL=C
export TZ=UTC
export RUSTFLAGS="--remap-path-prefix=${CONTRACTS_DIR}=/workspace/packages/contracts --remap-path-prefix=${HOME:-/root}=/cargo-home -C debuginfo=0 -C strip=symbols -C codegen-units=1"

echo "==> Building prerequisite contracts ($WASM_TARGET) with locked dependencies..."
cargo build --locked --release --target "$WASM_TARGET" -p yield-registry-contract
cargo build --locked --release --target "$WASM_TARGET" -p allocation-strategy-contract
cargo build --locked --release --target "$WASM_TARGET" -p vault-token-contract

for contract_toml in contracts/*/Cargo.toml; do
  pkg_name="$(grep '^name = ' "$contract_toml" | head -n 1 | sed 's/name = "\(.*\)"/\1/')"
  case "$pkg_name" in
    yield-registry-contract|allocation-strategy-contract|vault-token-contract|rent-escrow-contract)
      continue
      ;;
  esac
  echo "  Building $pkg_name..."
  cargo build --locked --release --target "$WASM_TARGET" -p "$pkg_name"
done

RELEASE_DIR="$CONTRACTS_DIR/target/$WASM_TARGET/release"
MANIFEST_FILE="$RELEASE_DIR/SHA256SUMS"
NOTES_FILE="$RELEASE_DIR/SHA256SUMS.md"

rm -f "$MANIFEST_FILE" "$NOTES_FILE"

if command -v stellar >/dev/null 2>&1; then
  echo "==> Optimizing WASM artifacts with stellar contract optimize..."
  for wasm in "$RELEASE_DIR"/*.wasm; do
    [[ "$wasm" == *_opt.wasm ]] && continue
    opt_wasm="${wasm%.wasm}_opt.wasm"
    stellar contract optimize --wasm "$wasm" --wasm-out "$opt_wasm" >/dev/null
  done
fi

echo "==> Generating deterministic SHA-256 manifest ($MANIFEST_FILE)..."
(
  cd "$RELEASE_DIR"
  for wasm in $(ls -1 *.wasm | LC_ALL=C sort); do
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$wasm" >> "$MANIFEST_FILE"
    else
      shasum -a 256 "$wasm" >> "$MANIFEST_FILE"
    fi
  done
)

GIT_COMMIT="$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo "unknown")"
{
  echo "### Reproducible Contract WASM Checksums"
  echo ""
  echo "- **Commit**: \`$GIT_COMMIT\`"
  echo "- **Toolchain**: \`$ACTUAL_RUSTC\` (\`$WASM_TARGET\`)"
  echo "- **Container Image**: \`$PINNED_RUST_IMAGE\`"
  echo ""
  echo "| Artifact | SHA-256 |"
  echo "| :--- | :--- |"
  while read -r hash file; do
    echo "| \`$file\` | \`$hash\` |"
  done < "$MANIFEST_FILE"
} > "$NOTES_FILE"

cat "$MANIFEST_FILE"
echo "✓ Reproducible build complete. Checksums written to $MANIFEST_FILE and $NOTES_FILE"
