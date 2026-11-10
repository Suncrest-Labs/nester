"use client";

import { formatDistanceToNowStrict, isBefore } from "date-fns";
import type { LockedPosition } from "@/lib/types/progress";

function LockItem({ position }: { position: LockedPosition }) {
  const maturityDate = new Date(position.matures_at);
  const isMatured = isBefore(maturityDate, new Date());
  const distance = formatDistanceToNowStrict(maturityDate, { addSuffix: true });

  return (
    <li className="flex items-center justify-between gap-3 py-3">
      <div className="min-w-0">
        <p className="text-sm text-foreground">
          <span className="tabular-nums">
            {position.amount.toLocaleString()} {position.currency}
          </span>
          <span className="ml-2 text-[13px] tabular-nums text-muted-foreground">+{position.boost_percent}%</span>
        </p>
        <p className="text-[13px] text-muted-foreground">
          {isMatured ? `Matured ${distance}` : `Matures ${distance}`}
        </p>
      </div>
      {position.yield_earned > 0 && (
        <span className="shrink-0 text-sm tabular-nums text-foreground">
          +{position.yield_earned.toLocaleString()}
        </span>
      )}
    </li>
  );
}

export function MaturityTimeline({ positions }: { positions: LockedPosition[] }) {
  if (!positions.length) return null;

  const sorted = [...positions].sort(
    (a, b) => new Date(a.matures_at).getTime() - new Date(b.matures_at).getTime()
  );

  return (
    <div data-testid="maturity-timeline">
      <h3 className="text-sm font-medium text-muted-foreground">Locked positions</h3>
      <ul className="mt-2 divide-y">
        {sorted.map((pos) => (
          <LockItem key={pos.id} position={pos} />
        ))}
      </ul>
    </div>
  );
}
