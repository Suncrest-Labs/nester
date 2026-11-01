"use client";

import {
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Tooltip,
  type TooltipContentProps,
  type TooltipValueType,
} from "recharts";
import type { AssetComposition } from "@/lib/types/progress";

type TooltipNameType = number | string;

const TONES = [1, 0.55, 0.3, 0.15];

function tone(index: number): number {
  return TONES[index % TONES.length];
}

function CompositionTooltip({
  active,
  payload,
}: TooltipContentProps<TooltipValueType, TooltipNameType>) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload as AssetComposition;
  return (
    <div className="rounded-lg border bg-card px-3 py-2 text-xs">
      <p className="text-foreground">{d.asset}</p>
      <p className="tabular-nums text-muted-foreground">{d.percentage}% of the balance</p>
    </div>
  );
}

export function MultiAssetComposition({
  composition,
}: {
  composition: AssetComposition[];
}) {
  if (!composition?.length) return null;

  return (
    <div data-testid="multi-asset-composition">
      <h3 className="text-sm font-medium text-muted-foreground">Asset composition</h3>
      <div className="mt-3 flex items-center gap-4">
        <div className="h-24 w-24 shrink-0 text-foreground">
          <ResponsiveContainer width="100%" height="100%">
            <PieChart>
              <Pie
                data={composition}
                dataKey="percentage"
                nameKey="asset"
                cx="50%"
                cy="50%"
                innerRadius={28}
                outerRadius={44}
                paddingAngle={composition.length > 1 ? 2 : 0}
                strokeWidth={0}
              >
                {composition.map((entry, index) => (
                  <Cell key={entry.asset} fill="currentColor" fillOpacity={tone(index)} />
                ))}
              </Pie>
              <Tooltip content={CompositionTooltip} />
            </PieChart>
          </ResponsiveContainer>
        </div>
        <ul className="flex-1 space-y-1.5">
          {composition.map((a, index) => (
            <li key={a.asset} className="flex items-center justify-between gap-2 text-sm">
              <span className="flex min-w-0 items-center gap-2">
                <span
                  className="h-2 w-2 shrink-0 rounded-full bg-foreground"
                  style={{ opacity: tone(index) }}
                  aria-hidden="true"
                />
                <span className="truncate text-foreground/80">{a.asset}</span>
              </span>
              <span className="shrink-0 tabular-nums text-foreground">{a.percentage}%</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
