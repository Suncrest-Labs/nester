"use client";

import { useSession } from "next-auth/react";
import { useState, useEffect } from "react";
import PortfolioChart from "@/components/analytics/PortfolioChart";
import { VaultComparison } from "@/components/analytics/VaultComparison";
import { BenchmarkCard } from "@/components/analytics/BenchmarkCard";
import { AllocationPieChart } from "@/components/analytics/AllocationPieChart";
import { YieldBreakdownChart } from "@/components/analytics/YieldBreakdownChart";
import { PerformanceMetricsCards } from "@/components/analytics/PerformanceMetricsCards";

export default function AnalyticsPage() {
  const { data: session } = useSession();
  const [analyticsData, setAnalyticsData] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [timeRange, setTimeRange] = useState<string>("30"); // default 30 days

  useEffect(() => {
    if (!session?.user?.id) return;
    const fetchData = async () => {
      setLoading(true);
      setError(null);
      try {
        const from = new Date();
        from.setDate(from.getDate() - parseInt(timeRange));
        const to = new Date();
        const response = await fetch(
          `/api/v1/users/${session.user.id}/analytics?from=${from.toISOString().split('T')[0]}&to=${to.toISOString().split('T')[0]}`
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
  }, [session?.user?.id, timeRange]);

  if (loading) return <div className="flex h-[600px] items-center justify-center">Loading...</div>;
  if (error) return <div className="flex h-[600px] items-center justify-center text-red-500">{error}</div>;
  if (!analyticsData) return <div className="flex h-[600px] items-center justify-center">No data</div>;

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
      <PortfolioChart data={analyticsData.daily_snapshots} />

      {/* Section 2: Yield Breakdown */}
      <YieldBreakdownChart data={analyticsData.vault_monthly_yield} />

      {/* Section 3: Allocation Pie Chart */}
      <AllocationPieChart data={analyticsData.current_allocation} />

      {/* Section 4: Performance Metrics Cards */}
      <PerformanceMetricsCards data={analyticsData.performance_metrics} />

      {/* Section 5: Vault Comparison */}
      <VaultComparison vaults={analyticsData.vaults} />

      {/* Section 6: Benchmark Card */}
      <BenchmarkCard 
        userAPY={analyticsData.performance_metrics.average_apy}
        defiLlamaAPY={6.5} // placeholder, would come from API in real implementation
        nigeriaBankRate={3.5}
        nigeriaInflationRate={25}
      />
    </div>
  );
}