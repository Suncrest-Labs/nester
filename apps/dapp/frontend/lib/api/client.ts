import {
  getAccessToken,
  getOrCreateDeviceFingerprint,
  setAccessToken,
  clearTokens,
} from "@/lib/auth/token-store";

/**
 * Typed API client for the Nester Go backend.
 *
 * All routes under /api/v1/ require a Bearer JWT. The token is read from the
 * canonical token store (lib/auth/token-store) on every request so it always
 * reflects the current login state without needing to thread it through
 * props/context. Access tokens are short-lived (minutes); a 401 triggers a
 * single transparent refresh-and-retry so the short lifetime stays invisible
 * to callers.
 */

// ── Helpers ───────────────────────────────────────────────────────────────────

function getApiBase(): string {
  if (process.env.NEXT_PUBLIC_API_URL) {
    return process.env.NEXT_PUBLIC_API_URL;
  }
  // Use relative URL for browser (to leverage Next.js rewrites)
  // Use absolute URL for server-side
  return typeof window === "undefined"
    ? "http://localhost:8080/api/v1"
    : "/api/v1";
}

const API_BASE = getApiBase();

/** @deprecated import getAccessToken from "@/lib/auth/token-store" instead. */
export function getStoredToken(): string {
  return getAccessToken();
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type ApiEnvelope<T> = {
  success: boolean;
  data: T;
  error?: { code?: string; message: string };
};

// Single-flight guard: concurrent 401s share one in-flight /auth/refresh
// call. Without this, N concurrent requests hitting a stale access token
// would each attempt to rotate the refresh token, and the server's reuse
// detection would treat the losers as token theft and kill the session.
let refreshInFlight: Promise<{ access_token: string }> | null = null;

async function refreshTokens(): Promise<{ access_token: string }> {
  if (!refreshInFlight) {
    refreshInFlight = performRefresh().finally(() => {
      refreshInFlight = null;
    });
  }
  return refreshInFlight;
}

async function performRefresh(): Promise<{ access_token: string }> {
  // The refresh token itself is an httpOnly cookie set by the server — this
  // client never reads or sends it explicitly; `credentials: "include"`
  // makes the browser attach it (and store the rotated one from the
  // response) automatically.
  let res: Response;
  try {
    res = await fetch(`${API_BASE}/auth/refresh`, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        device_fingerprint: getOrCreateDeviceFingerprint(),
      }),
    });
  } catch {
    // Network failure — the refresh token was never actually rejected, so
    // don't tear down the session over a transient connectivity blip.
    throw new ApiError(0, "NETWORK_ERROR", "Could not reach the server to refresh the session");
  }

  const body = await res.text();
  let json: ApiEnvelope<{ access_token: string }> | null = null;
  if (body.trim()) {
    try {
      json = JSON.parse(body) as ApiEnvelope<{ access_token: string }>;
    } catch {
      json = null;
    }
  }

  if (res.status === 401 || res.status === 403) {
    // The refresh token itself was rejected (expired, reused, revoked,
    // device mismatch) — this is a genuine end of session.
    clearTokens();
    throw new ApiError(
      res.status,
      json?.error?.code ?? "SESSION_EXPIRED",
      json?.error?.message ?? "Session expired, please sign in again"
    );
  }

  if (!res.ok || !json?.success) {
    // 5xx / malformed response — transient. The refresh token may still be
    // valid, so preserve the local session rather than forcing a re-login;
    // let the caller (reactive retry or the proactive-refresh timer) try
    // again.
    throw new ApiError(
      res.status,
      json?.error?.code ?? "REFRESH_UNAVAILABLE",
      json?.error?.message ?? "Could not refresh the session, please try again"
    );
  }

  setAccessToken(json.data.access_token);
  return json.data;
}

export async function apiRequest<T>(
  path: string,
  init?: RequestInit
): Promise<T> {
  return apiFetch<T>(path, init);
}

async function apiFetch<T>(
  path: string,
  init?: RequestInit & { skipAuth?: boolean; _isRetry?: boolean }
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init?.headers as Record<string, string>),
  };

  if (!init?.skipAuth) {
    const token = getAccessToken();
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }
  }

  const res = await fetch(`${API_BASE}${path}`, {
    // Needed so /auth/verify, /auth/logout(-all) can set/clear the httpOnly
    // refresh cookie via Set-Cookie — harmless for every other route.
    credentials: "include",
    ...init,
    headers,
  });

  // A 401 on an authenticated request means the access token expired (it's
  // short-lived by design) — transparently refresh once and retry, rather
  // than surfacing a spurious failure to the caller.
  if (res.status === 401 && !init?.skipAuth && !init?._isRetry) {
    try {
      await refreshTokens();
    } catch (refreshErr) {
      throw refreshErr instanceof ApiError
        ? refreshErr
        : new ApiError(401, "SESSION_EXPIRED", "Session expired, please sign in again");
    }
    return apiFetch<T>(path, { ...init, _isRetry: true });
  }

  // Handle non-JSON or empty responses
  const body = await res.text();
  let json: ApiEnvelope<T> | null = null;

  if (body.trim()) {
    try {
      json = JSON.parse(body) as ApiEnvelope<T>;
    } catch {
      if (!res.ok) {
        throw new ApiError(
          res.status,
          "INVALID_RESPONSE",
          `API returned a non-JSON response`
        );
      }
    }
  }

  if (!res.ok) {
    throw new ApiError(
      res.status,
      json?.error?.code ?? "UNKNOWN",
      json?.error?.message ??
        `API error ${res.status}${res.statusText ? ` ${res.statusText}` : ""}`
    );
  }

  if (!json?.success) {
    throw new ApiError(
      res.status,
      json?.error?.code ?? "UNKNOWN",
      json?.error?.message ?? `API error ${res.status}`
    );
  }

  return json.data as T;
}

// ── Domain types ──────────────────────────────────────────────────────────────

export interface ApiVault {
  id: string;
  user_id: string;
  contract_address: string;
  total_deposited: string;
  current_balance: string;
  currency: string;
  status: "active" | "paused" | "closed";
  yield_earned: string;
  fees_paid: string;
  last_synced_at?: string;
  allocations?: ApiAllocation[];
  created_at: string;
  updated_at: string;
}

export interface ApiAllocation {
  id: string;
  vault_id: string;
  protocol: string;
  amount: string;
  apy: string;
  status: string;
  allocated_at: string;
  updated_at?: string;
}

export interface ApiSettlement {
  id: string;
  user_id: string;
  vault_id: string;
  amount: string;
  currency: string;
  fiat_currency: string;
  fiat_amount: string;
  exchange_rate: string;
  destination: {
    type: string;
    provider: string;
    account_number: string;
    account_name: string;
    bank_code?: string;
  };
  status:
    | "initiated"
    | "liquidity_matched"
    | "fiat_dispatched"
    | "confirmed"
    | "failed";
  retry_count: number;
  error_message?: string;
  notes?: string;
  estimated_fee?: string;
  created_at: string;
  completed_at?: string;
}

export interface ApiUser {
  id: string;
  wallet_address: string;
  display_name: string;
  created_at: string;
  updated_at: string;
}

export interface ApiPerformanceSummary {
  vault_id: string;
  current_balance: number;
  total_deposited: number;
  total_yield: number;
  roi_pct: number;
  apy_7d: number;
  apy_30d: number;
  apy_90d: number;
  snapshot_count: number;
}

export interface ApiPerformanceSnapshot {
  id: string;
  vault_id: string;
  balance: number;
  apy: number;
  recorded_at: string;
}

export interface ApiTransaction {
  id: string;
  vault_id: string;
  type: "deposit" | "withdrawal" | "settlement";
  amount: string;
  currency: string;
  tx_hash: string;
  created_at: string;
}

// Auth types
export interface ChallengeResponse {
  challenge: string;
}

// The refresh token is never present here — the server sets it as an
// httpOnly cookie instead (see lib/auth/token-store.ts).
export interface TokenResponse {
  access_token: string;
  expires_in: number;
  token_type: string;
}

export interface SessionView {
  id: string;
  device_fingerprint: string;
  user_agent?: string;
  ip_address?: string;
  created_at: string;
  last_active_at: string;
  absolute_expires_at: string;
  is_current: boolean;
}

// ── API surface ───────────────────────────────────────────────────────────────

export const api = {
  /** Challenge / verify wallet login, session refresh + device management */
  auth: {
    requestChallenge: (walletAddress: string) =>
      apiFetch<ChallengeResponse>("/auth/challenge", {
        method: "POST",
        body: JSON.stringify({ wallet_address: walletAddress }),
        skipAuth: true,
      }),

    verify: (walletAddress: string, signature: string, challenge: string) =>
      apiFetch<TokenResponse>("/auth/verify", {
        method: "POST",
        body: JSON.stringify({
          wallet_address: walletAddress,
          signature,
          challenge,
          device_fingerprint: getOrCreateDeviceFingerprint(),
        }),
        skipAuth: true,
      }),

    /** Manually trigger a refresh; apiFetch already does this transparently on 401. */
    refresh: () => refreshTokens(),

    logout: () => apiFetch<void>("/auth/logout", { method: "POST" }),

    logoutAll: () => apiFetch<{ revoked_count: number }>("/auth/logout-all", { method: "POST" }),

    listSessions: () => apiFetch<{ sessions: SessionView[] }>("/auth/sessions"),

    revokeSession: (id: string) =>
      apiFetch<void>(`/auth/sessions/${id}`, { method: "DELETE" }),
  },

  /** User lookups */
  users: {
    getByWallet: (address: string) =>
      apiFetch<ApiUser>(`/users/wallet/${address}`),

    getById: (id: string) =>
      apiFetch<ApiUser>(`/users/${id}`),

    register: (walletAddress: string, displayName: string) =>
      apiFetch<ApiUser>("/users", {
        method: "POST",
        body: JSON.stringify({ wallet_address: walletAddress, display_name: displayName }),
        skipAuth: true,
      }),
  },

  /** Vault CRUD */
  vaults: {
    list: (userId?: string) =>
      apiFetch<ApiVault[]>(userId ? `/vaults?userId=${userId}` : "/vaults"),

    getById: (vaultId: string) =>
      apiFetch<ApiVault>(`/vaults/${vaultId}`),

    getAllocations: (vaultId: string) =>
      apiFetch<ApiAllocation[]>(`/vaults/${vaultId}/allocations`),

    create: (contractAddress: string, currency: string) =>
      apiFetch<ApiVault>("/vaults", {
        method: "POST",
        body: JSON.stringify({ contract_address: contractAddress, currency }),
      }),
  },

  /** Performance metrics */
  performance: {
    getSummary: (vaultId: string) =>
      apiFetch<ApiPerformanceSummary>(`/vaults/${vaultId}/performance`),

    getHistory: (vaultId: string, period = "30d") =>
      apiFetch<ApiPerformanceSnapshot[]>(
        `/vaults/${vaultId}/performance/history?period=${period}`
      ),

    getApy: (vaultId: string) =>
      apiFetch<Record<string, number>>(`/vaults/${vaultId}/performance/apy`),
  },

  /** Settlements */
  settlements: {
    list: (userId: string, status?: string) =>
      apiFetch<ApiSettlement[]>(
        `/settlements?userId=${userId}${status ? `&status=${status}` : ""}`
      ),

    getById: (settlementId: string) =>
      apiFetch<ApiSettlement>(`/settlements/${settlementId}`),

    create: (req: {
      user_id: string;
      vault_id: string;
      amount: string;
      currency: string;
      fiat_currency: string;
      fiat_amount: string;
      exchange_rate: string;
      destination: {
        type: string;
        provider: string;
        account_number: string;
        account_name: string;
        bank_code?: string;
      };
    }) =>
      apiFetch<ApiSettlement>("/settlements", {
        method: "POST",
        body: JSON.stringify(req),
      }),
  },
};
