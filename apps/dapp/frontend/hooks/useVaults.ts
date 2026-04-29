import { useQuery } from '@tanstack/react-query';

export interface Vault {
  id: string;
  name: string;
  strategy: string;
  contractAddress: string;
  minDeposit: number;
  apy?: number;
  tvl?: number;
}

export function formatTvl(value: number | undefined): string {
  if (value === undefined) return "TVL unavailable";
  if (value >= 1_000_000_000) return `$${(value / 1_000_000_000).toFixed(1)}B`;
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(0)}K`;
  return `$${value}`;
}

export function useVaults() {
  return useQuery({
    queryKey: ['vaults'],
    queryFn: async () => {
      const res = await fetch('/api/v1/vaults');
      if (!res.ok) throw new Error('Failed to fetch vaults');
      return res.json() as Promise<Vault[]>;
    },
    refetchInterval: 60000,
    staleTime: 30000,
  });
}
