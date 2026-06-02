import * as SecureStore from "expo-secure-store";

const runtime = globalThis as typeof globalThis & {
  process?: { env?: { EXPO_PUBLIC_API_URL?: string } };
};

const API_BASE_URL = runtime.process?.env?.EXPO_PUBLIC_API_URL ?? "http://localhost:8080";
const AUTH_TOKEN_KEY = "nester.authToken";

type ApiOptions = {
  method?: "GET" | "POST" | "PATCH";
  body?: unknown;
};

type ApiEnvelope<T> = {
  success: boolean;
  data: T;
  error?: { code: string; message: string };
};

export async function apiRequest<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const token = await SecureStore.getItemAsync(AUTH_TOKEN_KEY);
  const headers: Record<string, string> = {
    Accept: "application/json",
  };

  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: options.method ?? "GET",
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  const envelope = (await response.json()) as ApiEnvelope<T>;

  if (!response.ok || !envelope.success) {
    throw new Error(envelope.error?.message ?? `API request failed with ${response.status}`);
  }

  return envelope.data;
}
