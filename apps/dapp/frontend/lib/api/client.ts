import config from "@/lib/config";

export function getStoredToken(): string {
  if (typeof window === "undefined") return "";
  return localStorage.getItem("nester_token") ?? "";
}

type ApiEnvelope<T> = {
  success: boolean;
  data: T;
  error?: { message: string };
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
