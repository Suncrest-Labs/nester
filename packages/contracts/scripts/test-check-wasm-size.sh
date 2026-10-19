#!/usr/bin/env bash
# Tests for check-wasm-size.sh using throwaway files; needs no toolchain.
set -uo pipefail
cd "$(dirname "$0")"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail=0

expect() {
  local name="$1" want_rc="$2" want_text="$3"; shift 3
  local out rc
  out=$("$@" 2>&1); rc=$?
  if [ "$rc" -ne "$want_rc" ] || ! grep -qF -- "$want_text" <<<"$out"; then
    echo "FAIL: $name (rc=$rc, wanted rc=$want_rc and text: $want_text)"; echo "$out" | tail -4; fail=1
  else
    echo "ok:   $name"
  fi
}

mkdir "$tmp/ok" "$tmp/big" "$tmp/empty"
head -c 1000 /dev/zero > "$tmp/ok/a_opt.wasm"
head -c 131072 /dev/zero > "$tmp/ok/exact_opt.wasm"
head -c 1000 /dev/zero > "$tmp/big/a_opt.wasm"
head -c 131073 /dev/zero > "$tmp/big/vault_contract_opt.wasm"

expect "accepts files at or under the limit" 0 "within" bash ./check-wasm-size.sh "$tmp/ok"
expect "rejects a file one byte over the limit" 1 "vault_contract_opt.wasm" bash ./check-wasm-size.sh "$tmp/big"
expect "names the limit in the failure" 1 "131072" bash ./check-wasm-size.sh "$tmp/big"
expect "fails when there is nothing to check" 1 "no *_opt.wasm" bash ./check-wasm-size.sh "$tmp/empty"
expect "fails on a missing directory" 1 "not a directory" bash ./check-wasm-size.sh "$tmp/nope"

exit "$fail"
