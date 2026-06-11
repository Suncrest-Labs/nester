import { API_V1, apiAuthHeaders, getUserIdFromToken } from "@/lib/api/auth";

export interface SavedBankAccount {
  id: string;
  bank_name: string;
  bank_code?: string;
  account_last4: string;
  account_name: string;
  currency: string;
  country: string;
  is_default: boolean;
  verified_at?: string;
  created_at: string;
}

interface ApiEnvelope<T> {
  success: boolean;
  data?: T;
  error?: { code: string; message: string };
}

async function parseEnvelope<T>(res: Response): Promise<T> {
  const json = (await res.json()) as ApiEnvelope<T>;
  if (!res.ok || !json.success) {
    throw new Error(json.error?.message ?? `Request failed (${res.status})`);
  }
  return json.data as T;
}

function requireUserId(): string {
  const id = getUserIdFromToken();
  if (!id) {
    throw new Error("Sign in to manage saved bank accounts.");
  }
  return id;
}

export async function listBankAccounts(): Promise<SavedBankAccount[]> {
  const userId = requireUserId();
  const res = await fetch(`${API_V1}/users/${userId}/bank-accounts`, {
    headers: apiAuthHeaders(),
    cache: "no-store",
  });
  return parseEnvelope<SavedBankAccount[]>(res);
}

export async function addBankAccount(body: {
  bank_name: string;
  bank_code: string;
  account_number: string;
  account_name: string;
  currency: string;
  country: string;
  is_default?: boolean;
}): Promise<SavedBankAccount> {
  const userId = requireUserId();
  const res = await fetch(`${API_V1}/users/${userId}/bank-accounts`, {
    method: "POST",
    headers: apiAuthHeaders(),
    body: JSON.stringify(body),
  });
  return parseEnvelope<SavedBankAccount>(res);
}

export async function setDefaultBankAccount(acctId: string): Promise<SavedBankAccount> {
  const userId = requireUserId();
  const res = await fetch(`${API_V1}/users/${userId}/bank-accounts/${acctId}`, {
    method: "PATCH",
    headers: apiAuthHeaders(),
    body: JSON.stringify({ is_default: true }),
  });
  return parseEnvelope<SavedBankAccount>(res);
}

export async function removeBankAccount(acctId: string): Promise<void> {
  const userId = requireUserId();
  const res = await fetch(`${API_V1}/users/${userId}/bank-accounts/${acctId}`, {
    method: "DELETE",
    headers: apiAuthHeaders(),
  });
  await parseEnvelope<{ deleted: string }>(res);
}
