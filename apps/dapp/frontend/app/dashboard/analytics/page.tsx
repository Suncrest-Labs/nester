"use client";

import { useWallet } from "@/components/wallet-provider";
import { useState, useEffect } from "react";
import PortfolioChart from "@/components/analytics/PortfolioChart";
import { VaultComparison } from "@/components/analytics/VaultComparison";
import { AllocationPieChart } from "@/components/analytics/AllocationPieChart";
import { YieldBreakdownChart } from "@/components/analytics/YieldBreakdownChart";
import { PerformanceMetricsCards } from "@/components/analytics/PerformanceMetricsCards";
import { AnalyticsSkeleton } from "@/components/skeletons/page-skeletons";
import { EmptyState } from "@/components/ui/empty-state/empty-state";
import { BarChart3 } from "lucide-react";

interface AnalyticsData {
  daily_snapshots: {
    date: string;
    total_value_usd: number;
    yield_usd: number;
  }[];
  vault_monthly_yield: {
    vault_id: string;
    vault_name: string;
    month: string;
    yield_usd: number;
  }[];
  current_allocation: {
    vault_id: string;
    vault_name: string;
    value_usd: number;
    percentage: number;
  }[];
  performance_metrics: {
    average_apy: number;
    total_yield_usd: number;
    sharpe_ratio: number;
    sortino_ratio: number;
    max_drawdown: number;
  };
  vaults: {
    id: string;
    name: string;
    apy: number;
    tvl: number;
    risk_score: number;
  }[];
}

export default function AnalyticsPage() {
  const { address } = useWallet();
  const [analyticsData, setAnalyticsData] = useState<AnalyticsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [timeRange, setTimeRange] = useState<string>("30"); // default 30 days

  useEffect(() => {
    if (!address) return;
    const fetchData = async () => {
      setLoading(true);
      setError(null);
      try {
        const from = new Date();
        from.setDate(from.getDate() - parseInt(timeRange));
        const to = new Date();
        const response = await fetch(
          `/api/v1/analytics/users/${address}?from=${from.toISOString().split('T')[0]}&to=${to.toISOString().split('T')[0]}`
        );
        if (!response.ok) throw new Error("Failed to fetch analytics");
        const data = await response.json();
        setAnalyticsData(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unknown error");
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, [address, timeRange]);

  // Loading, error and empty are three distinct states: a skeleton while the
  // request is in flight, the route boundary for a failure, and an empty state
  // only once we know the response carried no snapshots.
  if (loading) return <AnalyticsSkeleton />;
  if (error) throw new Error(error);
  if (!analyticsData || analyticsData.daily_snapshots.length === 0) {
    return (
      <div data-testid="analytics-empty-state" className="p-6">
        <EmptyState
          icon={BarChart3}
          title="No analytics yet"
          description="Once you supply assets to a market we will chart your performance here."
          size="lg"
        />
      </div>
    );
  }

  const portfolioSnapshots = analyticsData.daily_snapshots.map((snapshot) => ({
    date: snapshot.date,
    total_balance_usd: snapshot.total_value_usd,
    yield_earned_usd: snapshot.yield_usd,
  }));
  const allocationByVaultId = new Map(
    analyticsData.current_allocation.map((item) => [item.vault_id, item])
  );
  const vaultsById = new Map(analyticsData.vaults.map((vault) => [vault.id, vault]));
  const allocationData = analyticsData.current_allocation.map((item) => ({
    protocol: item.vault_name,
    allocation_pct: item.percentage,
    balance_usd: item.value_usd,
    apy: vaultsById.get(item.vault_id)?.apy ?? 0,
  }));
  const performanceData = {
    total_yield_earned: analyticsData.performance_metrics.total_yield_usd,
    yield_change_pct: 0,
    best_vault_name:
      analyticsData.vaults.reduce(
        (best, vault) => (vault.apy > best.apy ? vault : best),
        analyticsData.vaults[0] ?? { name: "N/A", apy: 0 }
      ).name,
    best_vault_apy: Math.max(0, ...analyticsData.vaults.map((vault) => vault.apy)),
    average_apy: analyticsData.performance_metrics.average_apy,
    total_deposited: analyticsData.daily_snapshots.at(-1)?.total_value_usd ?? 0,
    total_withdrawn: 0,
    net_position: analyticsData.daily_snapshots.at(-1)?.total_value_usd ?? 0,
  };
  const vaultComparisonData = analyticsData.vaults.map((vault) => ({
    id: vault.id,
    name: vault.name,
    balance_usd: allocationByVaultId.get(vault.id)?.value_usd ?? 0,
    apy: vault.apy,
    yield_earned: 0,
    lock_period_days: 0,
  }));

  return (
    <div className="space-y-8 p-6">
      <div className="flex flex-wrap gap-4 mb-4">
        <button
          onClick={() => setTimeRange("7")}
          className={`px-3 py-1 rounded text-sm font-medium ${timeRange === "7" ? "bg-blue-600 text-white" : "bg-gray-200 hover:bg-gray-300"}`}
        >
          7D
        </button>
        <button
          onClick={() => setTimeRange("30")}
          className={`px-3 py-1 rounded text-sm font-medium ${timeRange === "30" ? "bg-blue-600 text-white" : "bg-gray-200 hover:bg-gray-300"}`}
        >
          1M
        </button>
        <button
          onClick={() => setTimeRange("90")}
          className={`px-3 py-1 rounded text-sm font-medium ${timeRange === "90" ? "bg-blue-600 text-white" : "bg-gray-200 hover:bg-gray-300"}`}
        >
          3M
        </button>
        <button
          onClick={() => setTimeRange("365")}
          className={`px-3 py-1 rounded text-sm font-medium ${timeRange === "365" ? "bg-blue-600 text-white" : "bg-gray-200 hover:bg-gray-300"}`}
        >
          1Y
        </button>
        <button
          onClick={() => setTimeRange("all")}
          className={`px-3 py-1 rounded text-sm font-medium ${timeRange === "all" ? "bg-blue-600 text-white" : "bg-gray-200 hover:bg-gray-300"}`}
        >
          All
        </button>
      </div>

      {/* Section 1: PortfolioChart */}
      <PortfolioChart data={portfolioSnapshots} />

      {/* Section 2: Yield Breakdown */}
      <YieldBreakdownChart data={analyticsData.vault_monthly_yield} />

      {/* Section 3: Allocation Pie Chart */}
      <AllocationPieChart data={allocationData} />

      {/* Section 4: Performance Metrics Cards */}
      <PerformanceMetricsCards data={performanceData} />

      {/* Section 5: Vault Comparison */}
      <VaultComparison vaults={vaultComparisonData} />

      {/* Section 6: Benchmark Card — external comparison rates (DeFi average,
          Nigerian bank rate, Nigerian inflation) have no live data source
          wired up yet. Rather than show fabricated numbers next to the
          user's real APY, this section is omitted until a real source is
          integrated (nester#1125). */}
    </div>
  );
}
