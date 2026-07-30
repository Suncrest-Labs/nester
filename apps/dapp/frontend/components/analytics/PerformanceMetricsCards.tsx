"use client";

import { Card } from "@/components/ui/card";

interface PerformanceMetrics {
  total_yield_earned: number;
  yield_change_pct: number;
  best_vault_name: string;
  best_vault_apy: number;
  average_apy: number;
  total_deposited: number;
  total_withdrawn: number;
  net_position: number;
}

interface PerformanceMetricsCardsProps {
  data: PerformanceMetrics;
}

export default function PerformanceMetricsCards({ data }: PerformanceMetricsCardsProps) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {/* Total Yield Earned */}
      <Card className="p-4">
        <h3 className="text-sm font-medium text-gray-500 mb-2">Total Yield Earned</h3>
        <p className="text-2xl font-bold">${data.total_yield_earned.toLocaleString(undefined, {
          minimumFractionDigits: 2,
          maximumFractionDigits: 2,
        })}</p>
        <p className="text-sm">
          {data.yield_change_pct >= 0 ? "+" : ""}
          {data.yield_change_pct.toFixed(1)}% vs last month
        </p>
      </Card>

      {/* Best Vault */}
      <Card className="p-4">
        <h3 className="text-sm font-medium text-gray-500 mb-2">Best Vault</h3>
        <p className="text-xl font-bold">{data.best_vault_name}</p>
        <p className="text-sm text-gray-600">@ {data.best_vault_apy.toFixed(1)}% APY</p>
      </Card>

      {/* Average APY */}
      <Card className="p-4">
        <h3 className="text-sm font-medium text-gray-500 mb-2">Average APY</h3>
        <p className="text-2xl font-bold">{data.average_apy.toFixed(1)}%</p>
      </Card>

      {/* Total Deposited */}
      <Card className="p-4">
        <h3 className="text-sm font-medium text-gray-500 mb-2">Total Deposited</h3>
        <p className="text-2xl font-bold">${data.total_deposited.toLocaleString(undefined, {
          minimumFractionDigits: 0,
          maximumFractionDigits: 0,
        })}</p>
      </Card>

      {/* Total Withdrawn */}
      <Card className="p-4">
        <h3 className="text-sm font-medium text-gray-500 mb-2">Total Withdrawn</h3>
        <p className="text-2xl font-bold">${data.total_withdrawn.toLocaleString(undefined, {
          minimumFractionDigits: 0,
          maximumFractionDigits: 0,
        })}</p>
      </Card>

      {/* Net Position */}
      <Card className="p-4">
        <h3 className="text-sm font-medium text-gray-500 mb-2">Net Position</h3>
        <p className="text-2xl font-bold">${data.net_position.toLocaleString(undefined, {
          minimumFractionDigits: 0,
          maximumFractionDigits: 0,
        })}</p>
      </Card>
    </div>
  );
}
export { PerformanceMetricsCards };