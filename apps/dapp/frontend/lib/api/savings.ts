import { apiRequest } from "@/lib/api/client";

export type SavingsPeriod = "7d" | "30d" | "90d";

export interface SavingsAdapter {
  address: string;
  name: string;
  value: string;
  absCap: string;
  relCapBps: number;
  share: string;
}

export interface SavingsPending {
  id: number;
  kind: string;
  /** Unix seconds at which anyone may execute the change. */
  eta: number;
}

export interface SavingsSummary {
  /** Trailing realized percent, e.g. "5.35"; null until enough history exists. */
  rate7d: string | null;
  rate30d: string | null;
  adapters: SavingsAdapter[];
  pending: SavingsPending[];
  reserve: { shares: string; value: string };
  sharePrice: string;
  totalAssets: string;
  idle: string;
  feeBps: number;
  timelockSeconds: number;
}

export interface SavingsHistoryPoint {
  takenAt: string;
  sharePrice: string;
}

export interface SavingsPosition {
  shares: string;
  value: string;
  /** Always null today: no per-wallet savings deposit data is indexed. */
  deposited: string | null;
}

export async function fetchSavingsSummary(): Promise<SavingsSummary> {
  const raw = await apiRequest<Partial<SavingsSummary>>("/savings");
  return {
    ...(raw as SavingsSummary),
    rate7d: raw.rate7d ?? null,
    rate30d: raw.rate30d ?? null,
    adapters: raw.adapters ?? [],
    pending: raw.pending ?? [],
  };
}

export async function fetchSavingsHistory(
  period: SavingsPeriod
): Promise<SavingsHistoryPoint[]> {
  const raw = await apiRequest<SavingsHistoryPoint[] | null>(
    `/savings/history?period=${period}`
  );
  return raw ?? [];
}

export async function fetchSavingsPosition(): Promise<SavingsPosition> {
  const raw = await apiRequest<SavingsPosition>("/savings/position");
  return { ...raw, deposited: raw.deposited ?? null };
}
