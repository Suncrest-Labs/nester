import Decimal from "decimal.js";
import { Card } from "@/components/ui/card";
import type { SavingsPosition } from "@/lib/api/savings";
import { MINUS, formatPercent, formatShares, formatUsdc } from "./format";

function positionChange(value: string, deposited: string) {
  const delta = new Decimal(value).minus(deposited);
  const base = new Decimal(deposited);
  const negative = delta.isNegative();
  const sign = negative ? MINUS : delta.isZero() ? "" : "+";
  const pct = base.isZero()
    ? ""
    : ` (${sign}${formatPercent(delta.abs().div(base).times(100), 2)})`;
  return { negative, text: `${sign}${formatUsdc(delta.abs().toString())}${pct}` };
}

export function PositionCard({ position }: { position: SavingsPosition }) {
  const change =
    position.deposited === null ? null : positionChange(position.value, position.deposited);

  return (
    <Card className="bg-card">
      <h3 className="text-sm font-medium text-muted-foreground">Your savings</h3>
      <p className="mt-2 font-heading text-4xl font-light tabular-nums text-foreground">
        {formatUsdc(position.value)}
      </p>
      <dl className="mt-4 space-y-2 text-sm">
        <div className="flex justify-between">
          <dt className="text-muted-foreground">Shares</dt>
          <dd className="tabular-nums text-foreground">{formatShares(position.shares)}</dd>
        </div>
        {change && (
          <div className="flex justify-between">
            <dt className="text-muted-foreground">Change since deposit</dt>
            <dd
              data-testid="position-change"
              className={`tabular-nums ${
                change.negative ? "text-red-600 dark:text-red-400" : "text-foreground"
              }`}
            >
              {change.text}
            </dd>
          </div>
        )}
      </dl>
    </Card>
  );
}
