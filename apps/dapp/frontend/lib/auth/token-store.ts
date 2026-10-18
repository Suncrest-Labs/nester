/**
 * Canonical client-side token storage (#1506).
 *
 * Security model:
 * - The access token is kept ONLY in module-level memory (`inMemoryAccessToken`)
 *   and is NEVER persisted to `window.localStorage` or `sessionStorage`.
 * - On page load / reload, `AuthProvider` obtains a fresh short-lived access
 *   token via `/auth/refresh` using the `httpOnly`, `Secure` refresh cookie.
 * - Any legacy `nester_auth_token` / `nester_token` entry in `localStorage` is
 *   purged on first load.
 */

const LEGACY_ACCESS_TOKEN_KEY = "nester_auth_token";
const LEGACY_TOKEN_ALT_KEY = "nester_token";
const USER_ID_KEY = "nester_user_id";
const DEVICE_FINGERPRINT_KEY = "nester_device_fingerprint";

let inMemoryAccessToken = "";
let legacyCleanedUp = false;

type TokenListener = (token: string | null) => void;
const listeners = new Set<TokenListener>();

function isBrowser(): boolean {
  return typeof window !== "undefined";
}

function notifyListeners(token: string | null): void {
  for (const listener of listeners) {
    listener(token);
  }
}

/**
 * Remove any legacy access token persisted in localStorage by older builds (#1506).
 */
export function cleanupLegacyAccessToken(): void {
  if (!isBrowser()) return;
  try {
    window.localStorage.removeItem(LEGACY_ACCESS_TOKEN_KEY);
    window.localStorage.removeItem(LEGACY_TOKEN_ALT_KEY);
    legacyCleanedUp = true;
  } catch {
    // Ignore storage access errors in restricted browsing modes
  }
}

export function subscribeAccessToken(listener: TokenListener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getAccessToken(): string {
  if (isBrowser() && !legacyCleanedUp) {
    cleanupLegacyAccessToken();
  }
  return inMemoryAccessToken;
}

export function getUserId(): string | null {
  if (!isBrowser()) return null;
  try {
    return window.localStorage.getItem(USER_ID_KEY);
  } catch {
    return null;
  }
}

export function setUserId(id: string | null): void {
  if (!isBrowser()) return;
  try {
    if (id) {
      window.localStorage.setItem(USER_ID_KEY, id);
    } else {
      window.localStorage.removeItem(USER_ID_KEY);
    }
  } catch {
    // Ignore storage errors
  }
}

export function setAccessToken(accessToken: string): void {
  if (isBrowser() && !legacyCleanedUp) {
    cleanupLegacyAccessToken();
  }
  inMemoryAccessToken = accessToken;
  notifyListeners(accessToken || null);
}

export function clearTokens(): void {
  inMemoryAccessToken = "";
  notifyListeners(null);
  if (!isBrowser()) return;
  try {
    window.localStorage.removeItem(LEGACY_ACCESS_TOKEN_KEY);
    window.localStorage.removeItem(LEGACY_TOKEN_ALT_KEY);
    window.localStorage.removeItem(USER_ID_KEY);
  } catch {
    // Ignore storage errors
  }
}

/**
 * A random ID generated once per browser and persisted indefinitely — not a
 * hardware fingerprint, just a stable per-installation identifier. Bound to
 * the session at login and enforced on every refresh server-side, so a
 * refresh token stolen and replayed from a different browser is detectable.
 */
export function getOrCreateDeviceFingerprint(): string {
  if (!isBrowser()) return "";
  try {
    let fingerprint = window.localStorage.getItem(DEVICE_FINGERPRINT_KEY);
    if (!fingerprint) {
      fingerprint = generateFingerprint();
      window.localStorage.setItem(DEVICE_FINGERPRINT_KEY, fingerprint);
    }
    return fingerprint;
  } catch {
    return generateFingerprint();
  }
}

function generateFingerprint(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `fp-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/** Legacy storage key exported for cleanup assertions. */
export const ACCESS_TOKEN_STORAGE_KEY = LEGACY_ACCESS_TOKEN_KEY;
export const USER_ID_STORAGE_KEY = USER_ID_KEY;
