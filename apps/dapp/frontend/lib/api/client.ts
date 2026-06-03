import config from "@/lib/config";

/**
 * Typed API client for the Nester Go backend.
 *
 * All routes under /api/v1/ require a Bearer JWT.
 * The token is read from the auth-store (localStorage) on every request so it
 * always reflects the current login state without needing to thread it through
 * props/context.
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

export function getStoredToken(): string {
  if (typeof window === "undefined") return "";
  return window.localStorage.getItem("nester_token") ?? "";
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

export async function apiRequest<T>(
  path: string,
  init?: RequestInit
): Promise<T> {
  const token = getStoredToken();
  const res = await fetch(`${config.apiUrl}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init?.headers,
    },
  });
  const json = (await res.json()) as ApiEnvelope<T>;
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `API error ${res.status}`);
  }
  return json.data;
}

async function apiFetch<T>(
  path: string,
  init?: RequestInit & { skipAuth?: boolean }
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init?.headers as Record<string, string>),
  };

  if (!init?.skipAuth) {
    const token = getStoredToken();
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers,
  });

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

export interface VerifyResponse {
  token: string;
}

// ── API surface ───────────────────────────────────────────────────────────────

export const api = {
  /** Challenge / verify wallet login */
  auth: {
    requestChallenge: (walletAddress: string) =>
      apiFetch<ChallengeResponse>("/auth/challenge", {
        method: "POST",
        body: JSON.stringify({ wallet_address: walletAddress }),
        skipAuth: true,
      }),

    verify: (
      walletAddress: string,
      signature: string,
      challenge: string
    ) =>
      apiFetch<VerifyResponse>("/auth/verify", {
        method: "POST",
        body: JSON.stringify({ wallet_address: walletAddress, signature, challenge }),
        skipAuth: true,
      }),
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
