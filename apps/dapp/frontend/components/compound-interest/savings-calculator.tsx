"use client";

import React, { useState, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Calculator, TrendingUp, Target, Download, FileText } from "lucide-react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  ComposedChart,
  Area,
} from "recharts";
import {
  projectionApi,
  formatProjectionAmount,
  formatProjectionAPY,
  formatSuccessProbability,
  type ProjectionInput,
  type SimulationInput,
} from "@/lib/api/projection";
import { useToast } from "@/components/ui/toast/toast-provider";
import { WidgetErrorBoundary } from "@/components/ui/error-boundary/error-boundary";
import { exportCsv } from "@/lib/export/csv";
import { exportPdf } from "@/lib/export/pdf";

interface SavingsCalculatorProps {
  className?: string;
}

export function SavingsCalculator({ className }: SavingsCalculatorProps) {
  const { error: showError } = useToast();
  
  const [formData, setFormData] = useState({
    initialDeposit: "1000",
    monthlyContribution: "200",
    apy: "0.08",
    periodMonths: 12,
    compoundFrequency: "monthly" as const,
    targetAmount: "",
    deadlineMonths: "",
  });

  const [shouldCalculate, setShouldCalculate] = useState(false);

  const projectionQuery = useQuery({
    queryKey: ['projection', formData],
    queryFn: () => {
      const input: ProjectionInput = {
        initial_deposit: formData.initialDeposit,
        monthly_contribution: formData.monthlyContribution,
        apy: formData.apy,
        period_months: formData.periodMonths,
        compound_frequency: formData.compoundFrequency,
      };
      return projectionApi.calculateProjection(input);
    },
    enabled: shouldCalculate && !!formData.initialDeposit && !!formData.apy,
  });

  // Monte Carlo probability forecast (#843): same inputs as the deterministic
  // projection above, plus an optional target/deadline to unlock
  // goal-success probability. Kept as a separate query (rather than folded
  // into projectionQuery) so a slow/failed simulation never blocks the
  // deterministic summary cards and chart from rendering.
  const simulationQuery = useQuery({
    queryKey: ['savings-simulation', formData],
    queryFn: () => {
      const deadlineMonths = parseInt(formData.deadlineMonths, 10);
      const input: SimulationInput = {
        initial_deposit: formData.initialDeposit,
        monthly_contribution: formData.monthlyContribution,
        apy: formData.apy,
        period_months: formData.periodMonths,
        compound_frequency: formData.compoundFrequency,
        ...(formData.targetAmount && { target_amount: formData.targetAmount }),
        ...(Number.isFinite(deadlineMonths) && deadlineMonths > 0 && { deadline_months: deadlineMonths }),
      };
      return projectionApi.simulateProjection(input);
    },
    enabled: shouldCalculate && !!formData.initialDeposit && !!formData.apy,
  });

  useEffect(() => {
    if (projectionQuery.isError) {
      showError("Failed to calculate projection", {
        title: "Calculation Error",
        action: {
          label: "Try again",
          onClick: () => setShouldCalculate(true),
        },
      });
    }
  }, [projectionQuery.isError, showError]);

  const handleInputChange = (field: string, value: string | number) => {
    setFormData(prev => ({ ...prev, [field]: value }));
    setShouldCalculate(false); // Reset calculation trigger
  };

  const handleCalculate = () => {
    // Validation
    if (!formData.initialDeposit || parseFloat(formData.initialDeposit) <= 0) {
      showError("Please enter a valid initial deposit amount");
      return;
    }
    
    if (!formData.apy || parseFloat(formData.apy) <= 0) {
      showError("Please enter a valid APY");
      return;
    }
    
    if (formData.periodMonths <= 0) {
      showError("Please enter a valid time period");
      return;
    }

    setShouldCalculate(true);
  };

  const chartData = projectionQuery.data?.timeline.map(point => ({
    month: point.month,
    principal: parseFloat(point.principal),
    total: parseFloat(point.total),
    yield: parseFloat(point.yield),
  })) || [];

  // Band chart data for the Monte Carlo forecast: `range` is a [low, high]
  // pair, which Recharts' <Area> renders as a filled band between the two
  // values rather than a fill down to the axis.
  const simChartData = simulationQuery.data?.timeline.map(point => ({
    month: point.month,
    range: [parseFloat(point.p10), parseFloat(point.p90)] as [number, number],
    p50: parseFloat(point.p50),
  })) || [];

  // Export helpers (#918): reuse the existing lib/export CSV/PDF utilities,
  // shaping each timeline point as a "transaction" row so the calculator
  // doesn't need its own export format.
  const projectionExportRows = (projectionQuery.data?.timeline ?? []).map((point) => ({
    date: `Month ${point.month}`,
    type: "Projected Balance",
    vault: "Savings Calculator",
    amount: parseFloat(point.total).toFixed(2),
    status: `Principal $${parseFloat(point.principal).toFixed(2)} / Yield $${parseFloat(point.yield).toFixed(2)}`,
  }));

  const handleExportCsv = () => {
    if (!projectionQuery.data) return;
    exportCsv(projectionExportRows, "savings-projection.csv");
  };

  const handleExportPdf = () => {
    if (!projectionQuery.data) return;
    const { summary } = projectionQuery.data;
    exportPdf({
      transactions: projectionExportRows,
      summary: {
        totalYield: parseFloat(summary.total_yield),
        totalDeposited: parseFloat(summary.total_deposited),
        totalWithdrawn: 0,
      },
      title: "Nester Savings Projection",
    });
  };

  const goalSuccess = simulationQuery.data?.goal_success;
  const successPct = goalSuccess ? goalSuccess.probability : null;
  const successTone =
    successPct === null
      ? null
      : successPct >= 0.7
        ? { bg: "bg-green-50", text: "text-green-700", strong: "text-green-800", label: "On track" }
        : successPct >= 0.4
          ? { bg: "bg-amber-50", text: "text-amber-700", strong: "text-amber-800", label: "At risk" }
          : { bg: "bg-red-50", text: "text-red-700", strong: "text-red-800", label: "Unlikely" };

  return (
    <WidgetErrorBoundary>
      <div className={`rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] p-8 ${className}`}>
        <div className="flex items-center gap-3 mb-6">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-blue-50">
            <Calculator className="h-5 w-5 text-blue-600" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-black dark:text-white">Savings Calculator</h3>
            <p className="text-sm text-black/60 dark:text-white/60">Plan your compound interest growth</p>
          </div>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Input Form */}
          <div className="space-y-6">
            <div>
              <label className="block text-sm font-medium text-black dark:text-white mb-2">
                Initial Deposit ($)
              </label>
              <input
                type="number"
                value={formData.initialDeposit}
                onChange={(e) => handleInputChange('initialDeposit', e.target.value)}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] dark:border-white/[0.08] focus:border-black/20 dark:focus:border-white/20 focus:outline-none"
                placeholder="1000"
                min="0"
                step="0.01"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-black dark:text-white mb-2">
                Monthly Contribution ($)
              </label>
              <input
                type="number"
                value={formData.monthlyContribution}
                onChange={(e) => handleInputChange('monthlyContribution', e.target.value)}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] dark:border-white/[0.08] focus:border-black/20 dark:focus:border-white/20 focus:outline-none"
                placeholder="200"
                min="0"
                step="0.01"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-black dark:text-white mb-2">
                Annual Percentage Yield (APY)
              </label>
              <div className="relative">
                <input
                  type="number"
                  value={(parseFloat(formData.apy) * 100).toString()}
                  onChange={(e) => handleInputChange('apy', (parseFloat(e.target.value) / 100).toString())}
                  className="w-full px-4 py-3 rounded-lg border border-black/[0.08] dark:border-white/[0.08] focus:border-black/20 dark:focus:border-white/20 focus:outline-none pr-8"
                  placeholder="8"
                  min="0"
                  max="100"
                  step="0.1"
                />
                <span className="absolute right-3 top-1/2 -translate-y-1/2 text-black/60 dark:text-white/60">%</span>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-black dark:text-white mb-2">
                Time Period (Months)
              </label>
              <input
                type="number"
                value={formData.periodMonths}
                onChange={(e) => handleInputChange('periodMonths', parseInt(e.target.value))}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] dark:border-white/[0.08] focus:border-black/20 dark:focus:border-white/20 focus:outline-none"
                placeholder="12"
                min="1"
                max="360"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-black dark:text-white mb-2">
                Compound Frequency
              </label>
              <select
                value={formData.compoundFrequency}
                onChange={(e) => handleInputChange('compoundFrequency', e.target.value)}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] dark:border-white/[0.08] focus:border-black/20 dark:focus:border-white/20 focus:outline-none"
              >
                <option value="monthly">Monthly</option>
                <option value="daily">Daily</option>
              </select>
            </div>

            <div className="pt-2 border-t border-black/[0.06] dark:border-white/[0.06]">
              <p className="text-xs font-medium text-black/50 dark:text-white/50 mb-4 uppercase tracking-wide">
                Optional: goal probability
              </p>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-black dark:text-white mb-2">
                    Target Amount ($)
                  </label>
                  <input
                    type="number"
                    value={formData.targetAmount}
                    onChange={(e) => handleInputChange('targetAmount', e.target.value)}
                    className="w-full px-4 py-3 rounded-lg border border-black/[0.08] dark:border-white/[0.08] focus:border-black/20 dark:focus:border-white/20 focus:outline-none"
                    placeholder="e.g. 10000"
                    min="0"
                    step="0.01"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-black dark:text-white mb-2">
                    Deadline (Months)
                  </label>
                  <input
                    type="number"
                    value={formData.deadlineMonths}
                    onChange={(e) => handleInputChange('deadlineMonths', e.target.value)}
                    className="w-full px-4 py-3 rounded-lg border border-black/[0.08] dark:border-white/[0.08] focus:border-black/20 dark:focus:border-white/20 focus:outline-none"
                    placeholder="e.g. 18"
                    min="1"
                    max="360"
                  />
                </div>
              </div>
            </div>

            <button
              onClick={handleCalculate}
              disabled={projectionQuery.isLoading}
              className="w-full bg-black dark:bg-blue-600 text-white py-3 rounded-lg font-medium hover:bg-black/90 dark:hover:bg-blue-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {projectionQuery.isLoading ? "Calculating..." : "Calculate Projection"}
            </button>
          </div>

          {/* Results */}
          <div className="space-y-6">
            {projectionQuery.data && (
              <>
                {/* Export actions (#918) */}
                <div className="flex items-center justify-end gap-2">
                  <button
                    onClick={handleExportCsv}
                    className="inline-flex items-center rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700"
                  >
                    <Download className="mr-1 h-4 w-4" /> CSV
                  </button>
                  <button
                    onClick={handleExportPdf}
                    className="inline-flex items-center rounded-md bg-gray-800 px-3 py-1.5 text-sm font-medium text-white hover:bg-gray-900"
                  >
                    <FileText className="mr-1 h-4 w-4" /> PDF
                  </button>
                </div>

                {/* Summary Cards */}
                <div className="grid grid-cols-2 gap-4">
                  <div className="bg-green-50 rounded-lg p-4">
                    <p className="text-sm text-green-700 mb-1">Final Balance</p>
                    <p className="text-xl font-bold text-green-800">
                      ${formatProjectionAmount(projectionQuery.data.summary.final_balance)}
                    </p>
                  </div>
                  <div className="bg-blue-50 rounded-lg p-4">
                    <p className="text-sm text-blue-700 mb-1">Total Yield</p>
                    <p className="text-xl font-bold text-blue-800">
                      ${formatProjectionAmount(projectionQuery.data.summary.total_yield)}
                    </p>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div className="bg-gray-50 rounded-lg p-4">
                    <p className="text-sm text-gray-700 mb-1">Total Deposited</p>
                    <p className="text-lg font-semibold text-gray-800">
                      ${formatProjectionAmount(projectionQuery.data.summary.total_deposited)}
                    </p>
                  </div>
                  <div className="bg-purple-50 rounded-lg p-4">
                    <p className="text-sm text-purple-700 mb-1">Effective APY</p>
                    <p className="text-lg font-semibold text-purple-800">
                      {formatProjectionAPY(projectionQuery.data.summary.effective_apy)}
                    </p>
                  </div>
                </div>

                {/* Chart */}
                <div className="h-64">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={chartData}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
                      <XAxis 
                        dataKey="month" 
                        stroke="#666"
                        fontSize={12}
                        tickLine={false}
                      />
                      <YAxis 
                        stroke="#666"
                        fontSize={12}
                        tickLine={false}
                        tickFormatter={(value) => `$${(value / 1000).toFixed(0)}k`}
                      />
                      <Tooltip 
                        formatter={(value, name) => [
                          typeof value === "number"
                            ? `$${value.toLocaleString("en-US", { minimumFractionDigits: 2 })}`
                            : String(value ?? ""),
                          String(name) === "total" ? "Total Balance" : String(name) === "principal" ? "Principal" : "Yield"
                        ]}
                        labelFormatter={(month) => `Month ${month}`}
                        contentStyle={{
                          backgroundColor: "white",
                          border: "1px solid #e5e7eb",
                          borderRadius: "8px",
                          fontSize: "12px"
                        }}
                      />
                      <Line 
                        type="monotone" 
                        dataKey="principal" 
                        stroke="#6b7280" 
                        strokeWidth={2}
                        dot={false}
                        name="Principal"
                      />
                      <Line 
                        type="monotone" 
                        dataKey="total" 
                        stroke="#3b82f6" 
                        strokeWidth={3}
                        dot={false}
                        name="Total"
                      />
                    </LineChart>
                  </ResponsiveContainer>
                </div>

                {/* Monte Carlo probability forecast (#843) */}
                {simulationQuery.data && (
                  <div className="pt-2 border-t border-black/[0.06] dark:border-white/[0.06]">
                    <div className="flex items-center gap-2 mb-3">
                      <Target className="h-4 w-4 text-black/50 dark:text-white/50" />
                      <h4 className="text-sm font-semibold text-black dark:text-white">
                        Probability Forecast
                      </h4>
                    </div>

                    {goalSuccess && successTone && (
                      <div className={`rounded-lg p-4 mb-4 ${successTone.bg}`}>
                        <div className="flex items-baseline justify-between">
                          <p className={`text-sm ${successTone.text}`}>
                            Chance of reaching ${formatProjectionAmount(goalSuccess.target_amount)} by month {goalSuccess.deadline_months}
                          </p>
                          <span className={`text-xs font-semibold ${successTone.text}`}>{successTone.label}</span>
                        </div>
                        <p className={`text-2xl font-bold ${successTone.strong}`}>
                          {formatSuccessProbability(goalSuccess.probability)}
                        </p>
                      </div>
                    )}

                    <div className="h-56">
                      <ResponsiveContainer width="100%" height="100%">
                        <ComposedChart data={simChartData}>
                          <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
                          <XAxis
                            dataKey="month"
                            stroke="#666"
                            fontSize={12}
                            tickLine={false}
                          />
                          <YAxis
                            stroke="#666"
                            fontSize={12}
                            tickLine={false}
                            tickFormatter={(value) => `$${(value / 1000).toFixed(0)}k`}
                          />
                          <Tooltip
                            formatter={(value, name) => {
                              if (name === "range" && Array.isArray(value)) {
                                const [lo, hi] = value as number[];
                                return [
                                  `$${lo.toLocaleString("en-US", { minimumFractionDigits: 0 })} – $${hi.toLocaleString("en-US", { minimumFractionDigits: 0 })}`,
                                  "P10–P90 range",
                                ];
                              }
                              return [
                                typeof value === "number"
                                  ? `$${value.toLocaleString("en-US", { minimumFractionDigits: 2 })}`
                                  : String(value ?? ""),
                                "Median (P50)",
                              ];
                            }}
                            labelFormatter={(month) => `Month ${month}`}
                            contentStyle={{
                              backgroundColor: "white",
                              border: "1px solid #e5e7eb",
                              borderRadius: "8px",
                              fontSize: "12px",
                            }}
                          />
                          <Legend
                            wrapperStyle={{ fontSize: "12px" }}
                            formatter={(value) => (value === "range" ? "P10–P90 range" : "Median (P50)")}
                          />
                          <Area
                            dataKey="range"
                            name="range"
                            stroke="none"
                            fill="#3b82f6"
                            fillOpacity={0.15}
                            isAnimationActive={false}
                          />
                          <Line
                            type="monotone"
                            dataKey="p50"
                            name="p50"
                            stroke="#3b82f6"
                            strokeWidth={2}
                            dot={false}
                          />
                        </ComposedChart>
                      </ResponsiveContainer>
                    </div>

                    <p className="text-xs text-black/40 dark:text-white/40 mt-3">
                      Based on {simulationQuery.data.path_count.toLocaleString()} simulated paths. Yield
                      volatility: {simulationQuery.data.volatility_source === "historical" ? "this vault's real APY history" : "a default estimate (no vault history available)"}.
                      Contribution reliability: {simulationQuery.data.contribution_source === "schedule" ? "your active savings schedule" : "a default assumption for new savers"}.
                    </p>
                  </div>
                )}
              </>
            )}

            {!projectionQuery.data && !projectionQuery.isLoading && (
              <div className="flex flex-col items-center justify-center py-12 text-center">
                <div className="flex h-16 w-16 items-center justify-center rounded-full bg-black/[0.04] dark:bg-white/[0.04] mb-4">
                  <TrendingUp className="h-8 w-8 text-black/40 dark:text-white/40" />
                </div>
                <h4 className="text-base font-medium text-black dark:text-white mb-2">
                  Ready to Calculate
                </h4>
                <p className="text-sm text-black/60 dark:text-white/60">
                  Enter your savings details and click "Calculate Projection" to see your growth potential.
                </p>
              </div>
            )}
          </div>
        </div>
      </div>
    </WidgetErrorBoundary>
  );
}