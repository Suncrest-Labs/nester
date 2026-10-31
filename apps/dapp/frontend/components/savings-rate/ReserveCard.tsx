import { Card } from "@/components/ui/card";
import { formatPercent, formatUsdc, percentOf } from "./format";

interface ReserveCardProps {
  reserve: { shares: string; value: string };
  totalAssets: string;
}

export function ReserveCard({ reserve, totalAssets }: ReserveCardProps) {
  return (
    <Card className="bg-card">
      <h3 className="text-sm font-medium text-muted-foreground">Reserve</h3>
      <p className="mt-2 font-heading text-3xl font-light tabular-nums text-foreground">
        {formatUsdc(reserve.value)}
      </p>
      <p className="mt-1 text-sm tabular-nums text-muted-foreground">
        {formatPercent(percentOf(reserve.value, totalAssets), 0)} of total assets
      </p>
      <p className="mt-4 text-sm text-foreground/80">
        Locked. Can only be used to cover saver losses, after a public timelock.
      </p>
    </Card>
  );
}
