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

function CompositionTooltip({
  active,
  payload,
}: TooltipContentProps<TooltipValueType, TooltipNameType>) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload as AssetComposition;
  return (
    <div className="rounded-xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-2.5 shadow-sm text-xs">
      <p className="font-medium text-black dark:text-white">{d.asset}</p>
      <p className="text-black/60 dark:text-white/60">{d.percentage}% of vault</p>
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
      <h3 className="text-xs font-semibold text-black/60 dark:text-white/60 uppercase tracking-wider mb-3">
        Asset Composition
      </h3>
      <div className="flex items-center gap-4">
        <div className="h-24 w-24 shrink-0">
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
                  <Cell key={index} fill={entry.color} />
                ))}
              </Pie>
              <Tooltip content={CompositionTooltip} />
            </PieChart>
          </ResponsiveContainer>
        </div>
        <div className="flex-1 space-y-1.5">
          {composition.map((a) => (
            <div key={a.asset} className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-2 min-w-0">
                <span
                  className="h-2 w-2 shrink-0 rounded-full"
                  style={{ background: a.color }}
                  aria-hidden="true"
                />
                <span className="text-xs text-black/70 dark:text-white/70 truncate">{a.asset}</span>
              </div>
              <span className="shrink-0 text-xs font-medium text-black dark:text-white">
                {a.percentage}%
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
