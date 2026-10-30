#!/usr/bin/env bash
# Usage: check-wasm-size.sh <max_bytes> <wasm>...
# Optimizes each WASM and fails if the optimized size exceeds <max_bytes>.
# Missing files are skipped with a notice.
set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <max_bytes> <wasm>..." >&2
  exit 2
fi

max="$1"
shift

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

if command -v stellar >/dev/null 2>&1; then
  optimize() { stellar contract optimize --wasm "$1" --wasm-out "$2" >/dev/null; }
elif command -v wasm-opt >/dev/null 2>&1; then
  optimize() { wasm-opt -Oz "$1" -o "$2"; }
else
  echo "need either the stellar CLI or wasm-opt (binaryen) on PATH" >&2
  exit 2
fi

status=0
for wasm in "$@"; do
  if [ ! -f "$wasm" ]; then
    echo "SKIP $wasm (not built yet)"
    continue
  fi
  out="$tmpdir/$(basename "$wasm")"
  optimize "$wasm" "$out"
  size="$(wc -c < "$out" | tr -d ' ')"
  if [ "$size" -gt "$max" ]; then
    echo "FAIL $wasm $size > $max"
    status=1
  else
    echo "OK   $wasm $size <= $max"
  fi
done
exit "$status"
