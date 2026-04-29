import { formatTvl } from "@/hooks/useVaults";

interface VaultMetricsProps {
  currentApy: number;
  tvl: number;
  userCount: number;
}

export function VaultMetrics({ currentApy, tvl, userCount }: VaultMetricsProps) {
  return (
    <div className="grid grid-cols-3 gap-3">
      <div className="rounded-2xl border border-border bg-white p-4">
        <p className="text-[11px] text-muted-foreground uppercase tracking-wider mb-1">
          APY
        </p>
        <p className="text-xl font-heading font-light text-emerald-600">
          {currentApy.toFixed(1)}%
        </p>
      </div>
      <div className="rounded-2xl border border-border bg-white p-4">
        <p className="text-[11px] text-muted-foreground uppercase tracking-wider mb-1">
          TVL
        </p>
        <p className="text-xl font-heading font-light text-foreground">
          {formatTvl(tvl)}
        </p>
      </div>
      <div className="rounded-2xl border border-border bg-white p-4">
        <p className="text-[11px] text-muted-foreground uppercase tracking-wider mb-1">
          Users
        </p>
        <p className="text-xl font-heading font-light text-foreground">
          {userCount >= 1000
            ? `${(userCount / 1000).toFixed(1)}k`
            : userCount}
        </p>
      </div>
    </div>
  );
}
