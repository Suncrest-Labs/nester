"use client";

import { useState } from "react";
import Decimal from "decimal.js";
import { useQuery } from "@tanstack/react-query";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { fetchSavingsHistory, type SavingsPeriod } from "@/lib/api/savings";
import { PANEL, SegmentedControl, SKELETON } from "@/components/ui/page-kit";

const PERIODS: { id: SavingsPeriod; label: string }[] = [
  { id: "7d", label: "7D" },
  { id: "30d", label: "30D" },
  { id: "90d", label: "90D" },
];

interface Point {
  t: number;
  price: number;
  label: string;
}

function formatPrice(value: Decimal.Value) {
  return new Decimal(value).toDecimalPlaces(6, Decimal.ROUND_HALF_UP).toFixed(6);
}

const dateTime = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});
const dateOnly = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });

export function useSharePriceHistory(period: SavingsPeriod) {
  return useQuery({
    queryKey: ["savings", "history", period],
    queryFn: () => fetchSavingsHistory(period),
    staleTime: 60_000,
  });
}

function ChartTooltip({ active, payload }: { active?: boolean; payload?: { payload: Point }[] }) {
  const point = active ? payload?.[0]?.payload : undefined;
  if (!point) return null;
  return (
    <div className="rounded-lg border bg-card px-3 py-2 text-[13px]">
      <p className="text-muted-foreground">{point.label}</p>
      <p className="mt-0.5 tabular-nums text-foreground">{formatPrice(point.price)}</p>
    </div>
  );
}

export function SharePriceChart() {
  const [period, setPeriod] = useState<SavingsPeriod>("30d");
  const { data, isLoading, error } = useSharePriceHistory(period);

  const points: Point[] = (data ?? [])
    .map((p) => {
      const t = new Date(p.takenAt).getTime();
      return { t, price: Number(p.sharePrice), label: dateTime.format(t) };
    })
    .filter((p) => Number.isFinite(p.t) && Number.isFinite(p.price))
    .sort((a, b) => a.t - b.t);
  const latest = points[points.length - 1];

  return (
    <div className={PANEL}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 className="text-sm font-medium text-muted-foreground">Share price</h3>
          {latest && (
            <p className="mt-2 font-heading text-3xl font-light tabular-nums text-foreground">
              {formatPrice(latest.price)}
            </p>
          )}
        </div>
        <SegmentedControl label="Share price period" options={PERIODS} value={period} onChange={setPeriod} />
      </div>

      <div className="mt-6">
        {isLoading ? (
          <div data-testid="share-price-loading" aria-busy="true" className={`h-48 ${SKELETON}`} />
        ) : error ? (
          <p className="text-sm text-muted-foreground">
            Share price history couldn&apos;t be loaded. Reload the page to try again.
          </p>
        ) : points.length < 2 ? (
          <p className="text-sm text-muted-foreground">The chart fills in as hourly snapshots arrive.</p>
        ) : (
          <div className="h-48 text-foreground" data-testid="share-price-chart">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={points} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
                <XAxis
                  dataKey="t"
                  type="number"
                  scale="time"
                  domain={["dataMin", "dataMax"]}
                  tickFormatter={(t: number) => dateOnly.format(t)}
                  tick={{ fontSize: 12, fill: "hsl(var(--muted-foreground))" }}
                  tickLine={false}
                  axisLine={false}
                  minTickGap={48}
                />
                <YAxis dataKey="price" domain={["auto", "auto"]} hide />
                <Tooltip
                  content={<ChartTooltip />}
                  cursor={{ stroke: "currentColor", strokeOpacity: 0.2 }}
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="price"
                  stroke="currentColor"
                  strokeWidth={2}
                  fill="currentColor"
                  fillOpacity={0.06}
                  activeDot={{ r: 4, fill: "currentColor", stroke: "hsl(var(--card))", strokeWidth: 2 }}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </div>
    </div>
  );
}
