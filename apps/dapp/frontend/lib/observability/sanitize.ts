/**
 * Redaction helpers for client error telemetry.
 *
 * The DApp handles wallet addresses, balances and bearer tokens. None of that
 * may leave the browser inside an error payload, so every string that reaches
 * the reporter is scrubbed before it is serialised.
 */

export const REDACTED = "[redacted]";

/** Stellar public keys (G...) and contract ids (C...) are 56 chars, base32. */
const STELLAR_ADDRESS = /\b[GC][A-Z2-7]{55}\b/g;
/** Stellar secret seeds (S...). Same shape, redacted for the same reason. */
const STELLAR_SECRET = /\b[S][A-Z2-7]{55}\b/g;
/** Muxed accounts (M...) are 69 chars. */
const STELLAR_MUXED = /\b[M][A-Z2-7]{68}\b/g;
/** Hex transaction hashes / raw XDR-ish blobs. Bounded to avoid backtracking. */
const HEX_BLOB = /\b[0-9a-fA-F]{40,128}\b/g;
/** `Bearer eyJ...` and bare JWTs. */
const BEARER = /\bBearer\s+[\w-]+\.[\w-]+\.[\w-]+/gi;
const JWT = /\b[\w-]{8,256}\.[\w-]{8,1024}\.[\w-]{8,256}\b/g;
/**
 * Anything that looks like a currency amount, with or without a symbol.
 * Quantifiers are bounded so the pattern cannot backtrack pathologically on a
 * long, attacker-influenced error message.
 */
const AMOUNT = /[$€£₦]\s?[\d,]{1,24}(?:\.\d{1,8})?|\b\d{1,15}\.\d{1,8}\b/g;

/**
 * Keys whose values are dropped wholesale rather than pattern-matched, because
 * their contents are sensitive regardless of shape.
 */
const DENIED_KEYS = [
  "address",
  "publickey",
  "publicKey",
  "secret",
  "seed",
  "privatekey",
  "mnemonic",
  "balance",
  "balances",
  "amount",
  "amounts",
  "total",
  "value",
  "usd",
  "ngn",
  "portfolio",
  "position",
  "positions",
  "transaction",
  "transactions",
  "tx",
  "xdr",
  "signature",
  "token",
  "accesstoken",
  "refreshtoken",
  "authorization",
  "auth",
  "cookie",
  "session",
  "email",
  "phone",
  "password",
  "apikey",
  "wallet",
  "account",
].map((k) => k.toLowerCase());

function isDeniedKey(key: string): boolean {
  const lower = key.toLowerCase();
  return DENIED_KEYS.some((denied) => lower.includes(denied));
}

/** Scrub sensitive substrings out of a free-text value (error messages, URLs). */
export function sanitizeText(input: string): string {
  return input
    .replace(BEARER, `Bearer ${REDACTED}`)
    .replace(STELLAR_SECRET, REDACTED)
    .replace(STELLAR_MUXED, REDACTED)
    .replace(STELLAR_ADDRESS, REDACTED)
    .replace(JWT, REDACTED)
    .replace(HEX_BLOB, REDACTED)
    .replace(AMOUNT, REDACTED);
}

/**
 * Strip query strings and dynamic ids from a pathname so the reported route is
 * a stable, non-identifying template (`/savings/[id]`, not `/savings/GABC...`).
 */
export function sanitizeRoute(pathname: string): string {
  const [path] = pathname.split(/[?#]/);
  return path
    .split("/")
    .map((segment) => {
      if (!segment) return segment;
      if (/^[GCSM][A-Z2-7]{20,}$/.test(segment)) return "[id]";
      if (/^\d+$/.test(segment)) return "[id]";
      if (/^[0-9a-fA-F-]{16,}$/.test(segment)) return "[id]";
      return segment;
    })
    .join("/");
}

/**
 * Recursively sanitize an arbitrary context object: denied keys are dropped,
 * strings are scrubbed, and numbers are kept only when the key is safe.
 */
export function sanitizeContext(
  input: unknown,
  depth = 0
): Record<string, unknown> {
  if (depth > 3 || input === null || typeof input !== "object") return {};

  const out: Record<string, unknown> = {};
  for (const [key, raw] of Object.entries(input as Record<string, unknown>)) {
    if (isDeniedKey(key)) {
      out[key] = REDACTED;
      continue;
    }
    if (typeof raw === "string") {
      out[key] = sanitizeText(raw);
    } else if (typeof raw === "number" || typeof raw === "boolean") {
      out[key] = raw;
    } else if (raw && typeof raw === "object" && !Array.isArray(raw)) {
      out[key] = sanitizeContext(raw, depth + 1);
    }
  }
  return out;
}
