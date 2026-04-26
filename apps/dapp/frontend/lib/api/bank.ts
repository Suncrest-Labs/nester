// lib/api/bank.ts
// All bank-related API calls go through the Go API so provider secrets
// (Paystack / Flutterwave keys) are never exposed to the browser.

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export interface Bank {
  name: string;
  code: string;
  country: string;
}

export interface AccountInfo {
  account_number: string;
  account_name: string;
  bank_code: string;
  bank_name: string;
}

export type ResolveResult =
  | { status: "success"; data: AccountInfo }
  | { status: "not_found" }
  | { status: "provider_error" };

/**
 * Fetch the bank list for a country from the Go API (cached 24h server-side).
 * Throws on network errors.
 */
export async function fetchBankList(country = "NG"): Promise<Bank[]> {
  const res = await fetch(`${API_BASE}/api/v1/banks?country=${country}`, {
    // Cache in the browser for 1 hour — server refreshes every 24h.
    next: { revalidate: 3600 },
  });

  if (!res.ok) {
    throw new Error(`Failed to fetch bank list: ${res.status}`);
  }

  const json = await res.json();
  return (json.data ?? []) as Bank[];
}

/**
 * Resolve an account name via the Go API.
 * Never throws — returns a typed discriminated union instead so the caller
 * can render the correct UI state without try/catch at the call site.
 *
 * NOTE: account numbers are PII — do not log them anywhere.
 */
export async function resolveAccountName(
  accountNumber: string,
  bankCode: string,
  country = "NG"
): Promise<ResolveResult> {
  try {
    const res = await fetch(
      `${API_BASE}/api/v1/banks/resolve?account_number=${accountNumber}&bank_code=${bankCode}&country=${country}`,
      {
        headers: {
          // Include JWT so the per-user rate limit on the server can key by user ID.
          Authorization: `Bearer ${getStoredToken()}`,
        },
        // Never cache resolve calls — each one is a live lookup.
        cache: "no-store",
      }
    );

    if (res.status === 404) return { status: "not_found" };
    if (res.status === 429) return { status: "provider_error" }; // rate limited
    if (res.status === 503) return { status: "provider_error" }; // both providers down
    if (!res.ok) return { status: "provider_error" };

    const json = await res.json();
    if (!json.success) return { status: "not_found" };

    return { status: "success", data: json.data as AccountInfo };
  } catch {
    // Network error — treat as provider error so the UI shows the yellow warning.
    return { status: "provider_error" };
  }
}

/** Pull the JWT from wherever the app stores it (localStorage key used by auth). */
function getStoredToken(): string {
  if (typeof window === "undefined") return "";
  return localStorage.getItem("nester_token") ?? "";
}