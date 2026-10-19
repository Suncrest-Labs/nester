#!/usr/bin/env bash
# Fails if any optimized contract is larger than the network's contract size
# limit. Mainnet's contract_max_size_bytes is 131072 (read with
# `stellar network settings`, checked 2026-10-12); a larger contract is
# rejected at deploy time, so catch it before spending anything.
#
# Usage: check-wasm-size.sh [DIR]   (default: ./target/wasm32-unknown-unknown/release)
#        MAX_WASM_BYTES=...          override the limit
set -euo pipefail

dir="${1:-./target/wasm32-unknown-unknown/release}"
limit="${MAX_WASM_BYTES:-131072}"

[ -d "$dir" ] || { echo "ERROR: $dir is not a directory" >&2; exit 1; }

shopt -s nullglob
files=("$dir"/*_opt.wasm)
[ "${#files[@]}" -gt 0 ] || { echo "ERROR: no *_opt.wasm files in $dir; build and optimize first" >&2; exit 1; }

status=0
for f in "${files[@]}"; do
  size=$(wc -c < "$f" | tr -d ' ')
  if [ "$size" -gt "$limit" ]; then
    echo "ERROR: $(basename "$f") is $size bytes, over the $limit byte contract size limit" >&2
    status=1
  else
    echo "ok: $(basename "$f") $size bytes (within $limit)"
  fi
done
exit "$status"
