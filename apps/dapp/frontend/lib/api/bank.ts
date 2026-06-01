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

// Hardcoded list used as fallback when the API is unavailable (e.g. no provider keys configured).
// CBN bank codes from the NIBBS/NIP directory — kept in sync with Paystack's bank list.
const NG_BANKS_FALLBACK: Bank[] = [
  { name: "Access Bank", code: "044", country: "NG" },
  { name: "Citibank Nigeria", code: "023", country: "NG" },
  { name: "Ecobank Nigeria", code: "050", country: "NG" },
  { name: "Fidelity Bank", code: "070", country: "NG" },
  { name: "First Bank of Nigeria", code: "011", country: "NG" },
  { name: "First City Monument Bank (FCMB)", code: "214", country: "NG" },
  { name: "Guaranty Trust Bank (GTBank)", code: "058", country: "NG" },
  { name: "Heritage Bank", code: "030", country: "NG" },
  { name: "Keystone Bank", code: "082", country: "NG" },
  { name: "Polaris Bank", code: "076", country: "NG" },
  { name: "Providus Bank", code: "101", country: "NG" },
  { name: "Stanbic IBTC Bank", code: "221", country: "NG" },
  { name: "Standard Chartered Bank", code: "068", country: "NG" },
  { name: "Sterling Bank", code: "232", country: "NG" },
  { name: "Union Bank of Nigeria", code: "032", country: "NG" },
  { name: "United Bank for Africa (UBA)", code: "033", country: "NG" },
  { name: "Unity Bank", code: "215", country: "NG" },
  { name: "Wema Bank", code: "035", country: "NG" },
  { name: "Zenith Bank", code: "057", country: "NG" },
  { name: "Jaiz Bank", code: "301", country: "NG" },
  { name: "SunTrust Bank", code: "100", country: "NG" },
  { name: "Titan Trust Bank", code: "102", country: "NG" },
  { name: "Globus Bank", code: "103", country: "NG" },
  { name: "Taj Bank", code: "302", country: "NG" },
  { name: "Coronation Merchant Bank", code: "559", country: "NG" },
  { name: "FBNQuest Merchant Bank", code: "060", country: "NG" },
  { name: "FSDH Merchant Bank", code: "501", country: "NG" },
  { name: "Rand Merchant Bank", code: "502", country: "NG" },
  { name: "Kuda Bank", code: "090267", country: "NG" },
  { name: "OPay", code: "999992", country: "NG" },
  { name: "Moniepoint MFB", code: "50515", country: "NG" },
  { name: "PalmPay", code: "999991", country: "NG" },
  { name: "Carbon (Paylater)", code: "565", country: "NG" },
  { name: "VFD Microfinance Bank", code: "566", country: "NG" },
  { name: "Rubies Bank", code: "125", country: "NG" },
  { name: "Sparkle Microfinance Bank", code: "090325", country: "NG" },
  { name: "Fairmoney Microfinance Bank", code: "090560", country: "NG" },
  { name: "Paycom (Opay Digital Services)", code: "100004", country: "NG" },
  { name: "9Payment Service Bank (9PSB)", code: "120001", country: "NG" },
  { name: "Momo Payment Service Bank (MTN)", code: "120003", country: "NG" },
];

/**
 * Fetch the bank list from the Go API, falling back to the hardcoded list
 * if the API is unavailable (e.g. no provider keys configured in dev).
 */
export async function fetchBankList(country = "NG"): Promise<Bank[]> {
  try {
    const res = await fetch(`${API_BASE}/api/v1/banks?country=${country}`, {
      next: { revalidate: 3600 },
    });

    if (!res.ok) {
      return country === "NG" ? NG_BANKS_FALLBACK : [];
    }

    const json = await res.json();
    const list = (json.data ?? []) as Bank[];
    // If the API returns an empty list (e.g. empty DB), fall back to static list.
    return list.length > 0 ? list : (country === "NG" ? NG_BANKS_FALLBACK : []);
  } catch {
    return country === "NG" ? NG_BANKS_FALLBACK : [];
  }
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

    // 422: Paystack resolved but found no matching account (valid format, wrong combo).
    // 404: legacy fallback for older API versions.
    if (res.status === 422 || res.status === 404) return { status: "not_found" };
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