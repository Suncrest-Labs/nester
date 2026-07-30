import { config } from "@/lib/config";
import { getAccessToken } from "@/lib/auth/token-store";

/** @deprecated import getAccessToken from "@/lib/auth/token-store" instead. */
export function getStoredToken(): string {
  return getAccessToken();
}

/** JWT `sub` claim — the authenticated user UUID. */
export function getUserIdFromToken(): string | null {
  const token = getStoredToken();
  if (!token) return null;
  const parts = token.split(".");
  if (parts.length < 2) return null;
  try {
    const payload = JSON.parse(atob(parts[1].replace(/-/g, "+").replace(/_/g, "/")));
    return typeof payload.sub === "string" ? payload.sub : null;
  } catch {
    return null;
  }
}

export function apiAuthHeaders(): HeadersInit {
  const token = getStoredToken();
  const headers: HeadersInit = { "Content-Type": "application/json" };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

export const API_V1 = config.apiUrl.replace(/\/$/, "");
