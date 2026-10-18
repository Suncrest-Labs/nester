#!/usr/bin/env bash
# =============================================================================
# Nester Launch Manifest Helper & Gate (Issue #1501)
#
# Parses `packages/contracts/launch-manifest.toml` and enforces that only
# approved mainnet contracts and yield adapters can be built, audited, or
# deployed by the launch pipeline.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST_FILE="${LAUNCH_MANIFEST_PATH:-$(cd "$SCRIPT_DIR/.." && pwd)/launch-manifest.toml}"

if [ ! -f "$MANIFEST_FILE" ]; then
  echo "Error: launch manifest not found at $MANIFEST_FILE" >&2
  exit 1
fi

# Outputs tab-separated rows: section_type (contracts|adapters|excluded), key, package, wasm, max_wasm_bytes, audited_commit, reason
parse_manifest() {
  awk '
    function emit() {
      if (section_type != "" && key != "") {
        printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", section_type, key, pkg, wasm, max_bytes, commit, reason
      }
    }
    /^[[:space:]]*\[/ {
      emit()
      line = $0
      sub(/^[[:space:]]*\[/, "", line)
      sub(/\].*$/, "", line)
      section_type = ""
      key = ""
      pkg = "-"
      wasm = "-"
      max_bytes = "131072"
      commit = "-"
      reason = "-"
      if (line ~ /^(contracts|adapters|excluded)\.[a-zA-Z0-9_-]+$/) {
        split(line, parts, ".")
        section_type = parts[1]
        key = parts[2]
      }
      next
    }
    /^[[:space:]]*package[[:space:]]*=/ {
      val = $0
      sub(/^[^=]*=[[:space:]]*"/, "", val)
      sub(/".*$/, "", val)
      pkg = val
    }
    /^[[:space:]]*wasm[[:space:]]*=/ {
      val = $0
      sub(/^[^=]*=[[:space:]]*"/, "", val)
      sub(/".*$/, "", val)
      wasm = val
    }
    /^[[:space:]]*max_wasm_bytes[[:space:]]*=/ {
      val = $0
      sub(/^[^=]*=[[:space:]]*/, "", val)
      sub(/[[:space:]#].*$/, "", val)
      if (section_type != "") {
        max_bytes = val
      }
    }
    /^[[:space:]]*audited_commit[[:space:]]*=/ {
      val = $0
      sub(/^[^=]*=[[:space:]]*"/, "", val)
      sub(/".*$/, "", val)
      commit = val
    }
    /^[[:space:]]*reason[[:space:]]*=/ {
      val = $0
      sub(/^[^=]*=[[:space:]]*"/, "", val)
      sub(/".*$/, "", val)
      reason = val
    }
    END {
      emit()
    }
  ' "$MANIFEST_FILE"
}

normalize_wasm_stem() {
  local raw="$1"
  raw="$(basename "$raw")"
  raw="${raw%_opt.wasm}"
  raw="${raw%.wasm}"
  echo "$raw"
}

assert_contract_allowed() {
  local target="$1"
  local norm_target
  norm_target="$(normalize_wasm_stem "$target")"

  local found_section=""
  local found_key=""
  local found_commit=""
  local found_reason=""

  while IFS=$'\t' read -r sec key pkg wasm max_bytes commit reason; do
    [ -z "$sec" ] && continue
    local wasm_stem
    wasm_stem="$(normalize_wasm_stem "$wasm")"
    if [ "$target" = "$key" ] || [ "$target" = "$pkg" ] || [ "$target" = "$wasm" ] || \
       [ "$norm_target" = "$key" ] || [ "$norm_target" = "$pkg" ] || [ "$norm_target" = "$wasm_stem" ]; then
      found_section="$sec"
      found_key="$key"
      found_commit="$commit"
      found_reason="$reason"
      break
    fi
  done < <(parse_manifest)

  if [ "$found_section" = "excluded" ]; then
    echo "Error: contract '$target' ($found_key) is explicitly excluded from launch-manifest.toml: $found_reason" >&2
    return 1
  fi

  if [ "$found_section" != "contracts" ] && [ "$found_section" != "adapters" ]; then
    echo "Error: contract '$target' is not listed in launch-manifest.toml [contracts] or [adapters]. Refusing to deploy." >&2
    return 1
  fi

  if ! [[ "$found_commit" =~ ^[0-9a-f]{40}$ ]]; then
    echo "Error: contract '$found_key' in launch-manifest.toml is missing a valid 40-char audited_commit (got '$found_commit')." >&2
    return 1
  fi

  return 0
}

list_packages() {
  while IFS=$'\t' read -r sec key pkg wasm max_bytes commit reason; do
    if [ "$sec" = "contracts" ] || [ "$sec" = "adapters" ]; then
      echo "$pkg"
    fi
  done < <(parse_manifest)
}

list_wasms() {
  while IFS=$'\t' read -r sec key pkg wasm max_bytes commit reason; do
    if [ "$sec" = "contracts" ] || [ "$sec" = "adapters" ]; then
      echo "$wasm"
    fi
  done < <(parse_manifest)
}

list_entries() {
  while IFS=$'\t' read -r sec key pkg wasm max_bytes commit reason; do
    if [ "$sec" = "contracts" ] || [ "$sec" = "adapters" ]; then
      printf "%s\t%s\t%s\t%s\t%s\t%s\n" "$sec" "$key" "$pkg" "$wasm" "$max_bytes" "$commit"
    fi
  done < <(parse_manifest)
}

check_wasm_sizes() {
  local wasm_dir="$1"
  local failed=0
  while IFS=$'\t' read -r sec key pkg wasm max_bytes commit reason; do
    if [ "$sec" = "contracts" ] || [ "$sec" = "adapters" ]; then
      local wasm_path="$wasm_dir/$wasm"
      local opt_stem="${wasm%.wasm}_opt.wasm"
      if [ -f "$wasm_dir/$opt_stem" ]; then
        wasm_path="$wasm_dir/$opt_stem"
      fi
      if [ -f "$wasm_path" ]; then
        local size
        size="$(wc -c < "$wasm_path" | tr -d '[:space:]')"
        if [ "$size" -gt "$max_bytes" ]; then
          echo "Error: $key ($wasm_path) is $size bytes, exceeding manifest budget of $max_bytes bytes." >&2
          failed=1
        fi
      fi
    fi
  done < <(parse_manifest)
  return "$failed"
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  case "${1:-}" in
    --list-packages)
      list_packages
      ;;
    --list-wasms)
      list_wasms
      ;;
    --list-entries)
      list_entries
      ;;
    --assert-allowed)
      if [ -z "${2:-}" ]; then
        echo "Usage: $0 --assert-allowed <contract-name-or-wasm>" >&2
        exit 1
      fi
      assert_contract_allowed "$2"
      ;;
    --check-size)
      if [ -z "${2:-}" ]; then
        echo "Usage: $0 --check-size <wasm-dir>" >&2
        exit 1
      fi
      check_wasm_sizes "$2"
      ;;
    *)
      echo "Usage: $0 [--list-packages|--list-wasms|--list-entries|--assert-allowed <contract>|--check-size <wasm-dir>]" >&2
      exit 1
      ;;
  esac
fi
