import Decimal from "decimal.js";
import { Card } from "@/components/ui/card";
import type { SavingsAdapter, SavingsSummary } from "@/lib/api/savings";
import { formatPercent, formatUsdc, percentOf } from "./format";

/** Share of the effective cap in use (0-100), or null when the cap is zero. */
function capUsage(adapter: SavingsAdapter, totalAssets: string): number | null {
  const abs = new Decimal(adapter.absCap);
  const rel = new Decimal(totalAssets).times(adapter.relCapBps).div(10_000);
  const cap = Decimal.min(abs, rel);
  if (cap.lte(0)) return null;
  const used = new Decimal(adapter.value).div(cap).times(100);
  return Decimal.min(used, 100).toDecimalPlaces(0, Decimal.ROUND_HALF_UP).toNumber();
}

function Row({ name, value, percent }: { name: string; value: string; percent: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4 text-sm">
      <span className="text-foreground">{name}</span>
      <span className="tabular-nums text-muted-foreground">
        {formatUsdc(value)} <span className="ml-2 text-foreground">{percent}</span>
      </span>
    </div>
  );
}

export function AllocationCard({ summary }: { summary: SavingsSummary }) {
  return (
    <Card className="bg-card">
      <h3 className="text-sm font-medium text-muted-foreground">Where the money sits</h3>
      <ul className="mt-3 space-y-4">
        <li>
          <Row
            name="Idle"
            value={summary.idle}
            percent={formatPercent(percentOf(summary.idle, summary.totalAssets), 0)}
          />
        </li>
        {summary.adapters.map((adapter) => {
          const usage = capUsage(adapter, summary.totalAssets);
          return (
            <li key={adapter.address}>
              <Row
                name={adapter.name}
                value={adapter.value}
                percent={formatPercent(new Decimal(adapter.share).times(100), 0)}
              />
              <div className="mt-2 flex items-center gap-3">
                <div
                  role="progressbar"
                  aria-label={`${adapter.name} cap usage`}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={usage ?? 100}
                  className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted"
                >
                  <div
                    className="h-full rounded-full bg-foreground/70"
                    style={{ width: `${usage ?? 100}%` }}
                  />
                </div>
                <span className="w-24 text-right text-xs tabular-nums text-muted-foreground">
                  {usage === null ? "Cap is zero" : `${usage}% of cap`}
                </span>
              </div>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
