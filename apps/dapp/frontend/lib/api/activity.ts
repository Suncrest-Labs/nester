// lib/api/activity.ts
// Typed client for GET /api/v1/activity

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export type ActivityTransactionType =
    | "deposit"
    | "withdrawal"
    | "settlement"
    | "rebalance"
    | "yield_earned";

export type ActivityTransactionStatus = "pending" | "completed" | "failed";

export interface ActivityTransaction {
    id: string;
    vault_id: string;
    type: ActivityTransactionType;
    amount: string;
    currency: string;
    tx_hash: string;
    status: ActivityTransactionStatus;
    error_reason?: string;
    created_at: string;
    updated_at: string;
    confirmed_at?: string;
}

export interface ActivityPage {
    items: ActivityTransaction[];
    next_cursor: string;
    prev_cursor: string;
    total: number;
    total_deposited: string;
    total_withdrawn: string;
    total_yield_earned: string;
}

export interface ActivityFilters {
    userId: string;
    types?: ActivityTransactionType[];
    status?: ActivityTransactionStatus | "";
    from?: string;   // RFC3339
    to?: string;     // RFC3339
    cursor?: string;
    limit?: number;
    vaultId?: string;
    search?: string;
}

function getStoredToken(): string {
    if (typeof window === "undefined") return "";
    return localStorage.getItem("nester_token") ?? "";
}

export async function fetchActivity(filters: ActivityFilters): Promise<ActivityPage> {
    const params = new URLSearchParams();
    params.set("userId", filters.userId);
    if (filters.types?.length)  params.set("type", filters.types.join(","));
    if (filters.status)         params.set("status", filters.status);
    if (filters.from)           params.set("from", filters.from);
    if (filters.to)             params.set("to", filters.to);
    if (filters.cursor)         params.set("cursor", filters.cursor);
    if (filters.limit)          params.set("limit", String(filters.limit));
    if (filters.vaultId)        params.set("vaultId", filters.vaultId);
    if (filters.search)         params.set("search", filters.search);

    const res = await fetch(`${API_BASE}/api/v1/activity?${params.toString()}`, {
        headers: {
            Authorization: `Bearer ${getStoredToken()}`,
            "Content-Type": "application/json",
        },
        cache: "no-store",
    });

    if (!res.ok) {
        throw new Error(`Failed to fetch activity: ${res.status} ${res.statusText}`);
    }

    const json = await res.json() as { data: ActivityPage };
    return json.data;
}
