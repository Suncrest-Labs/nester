import { safeStorage } from "@/lib/storage";

/**
 * Shared localStorage key names (issue #1233).
 *
 * `NETWORK_STORAGE_KEY` in particular was previously a raw string literal
 * repeated across NetworkProvider.tsx, lib/stellar/transaction.ts,
 * app/portfolio/page.tsx, and utils/explorer.ts — every reader of the active
 * network must agree on the same key, or a typo in one copy silently reads
 * the wrong (or no) persisted network.
 */

/** Persisted active network id ("testnet" | "mainnet"). */
export const NETWORK_STORAGE_KEY = "nester_network_id";

/** Prefix for per-network cached portfolio data, purged on network switch. */
export const PORTFOLIO_CACHE_PREFIX = "nester_portfolio_v1:";

export type NetworkId = "testnet" | "mainnet";

function isNetworkId(value: unknown): value is NetworkId {
  return value === "testnet" || value === "mainnet";
}

/**
 * Reads the persisted network id, or `null` if unset/unrecognized.
 *
 * Tolerant of two encodings so migrating every call site to safeStorage.set
 * (which JSON-encodes) doesn't strand a value written by the OLD raw
 * `localStorage.setItem("nester_network_id", networkId)` call sites this
 * issue replaces: a legacy value is a bare, un-quoted string ("testnet"),
 * which safeStorage.get's JSON.parse would otherwise reject as corrupted
 * and wipe. Read via safeStorage.getRaw and accept both the JSON-quoted
 * form and the legacy bare form, rather than making every existing user's
 * saved network preference silently revert to the default.
 */
export function readNetworkId(): NetworkId | null {
  const raw = safeStorage.getRaw(NETWORK_STORAGE_KEY);
  if (raw === null) return null;
  if (isNetworkId(raw)) return raw; // legacy bare string
  try {
    const parsed = JSON.parse(raw) as unknown;
    return isNetworkId(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

/** Persists the network id via safeStorage (never throws). */
export function writeNetworkId(networkId: NetworkId): void {
  safeStorage.set(NETWORK_STORAGE_KEY, networkId);
}
