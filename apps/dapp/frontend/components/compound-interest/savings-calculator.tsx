"use client";

import React, { useState, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Calculator, TrendingUp } from "lucide-react";
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";
import { 
  projectionApi, 
  formatProjectionAmount, 
  formatProjectionAPY, 
  type ProjectionInput 
} from "@/lib/api/projection";
import { useToast } from "@/components/ui/toast/toast-provider";
import { WidgetErrorBoundary } from "@/components/ui/error-boundary/error-boundary";

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

  return (
    <WidgetErrorBoundary>
      <div className={`rounded-2xl border border-black/[0.06] bg-white p-8 ${className}`}>
        <div className="flex items-center gap-3 mb-6">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-blue-50">
            <Calculator className="h-5 w-5 text-blue-600" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-black">Savings Calculator</h3>
            <p className="text-sm text-black/60">Plan your compound interest growth</p>
          </div>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Input Form */}
          <div className="space-y-6">
            <div>
              <label className="block text-sm font-medium text-black mb-2">
                Initial Deposit ($)
              </label>
              <input
                type="number"
                value={formData.initialDeposit}
                onChange={(e) => handleInputChange('initialDeposit', e.target.value)}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] focus:border-black/20 focus:outline-none"
                placeholder="1000"
                min="0"
                step="0.01"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-black mb-2">
                Monthly Contribution ($)
              </label>
              <input
                type="number"
                value={formData.monthlyContribution}
                onChange={(e) => handleInputChange('monthlyContribution', e.target.value)}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] focus:border-black/20 focus:outline-none"
                placeholder="200"
                min="0"
                step="0.01"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-black mb-2">
                Annual Percentage Yield (APY)
              </label>
              <div className="relative">
                <input
                  type="number"
                  value={(parseFloat(formData.apy) * 100).toString()}
                  onChange={(e) => handleInputChange('apy', (parseFloat(e.target.value) / 100).toString())}
                  className="w-full px-4 py-3 rounded-lg border border-black/[0.08] focus:border-black/20 focus:outline-none pr-8"
                  placeholder="8"
                  min="0"
                  max="100"
                  step="0.1"
                />
                <span className="absolute right-3 top-1/2 -translate-y-1/2 text-black/60">%</span>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-black mb-2">
                Time Period (Months)
              </label>
              <input
                type="number"
                value={formData.periodMonths}
                onChange={(e) => handleInputChange('periodMonths', parseInt(e.target.value))}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] focus:border-black/20 focus:outline-none"
                placeholder="12"
                min="1"
                max="360"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-black mb-2">
                Compound Frequency
              </label>
              <select
                value={formData.compoundFrequency}
                onChange={(e) => handleInputChange('compoundFrequency', e.target.value)}
                className="w-full px-4 py-3 rounded-lg border border-black/[0.08] focus:border-black/20 focus:outline-none"
              >
                <option value="monthly">Monthly</option>
                <option value="daily">Daily</option>
              </select>
            </div>

            <button
              onClick={handleCalculate}
              disabled={projectionQuery.isLoading}
              className="w-full bg-black text-white py-3 rounded-lg font-medium hover:bg-black/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {projectionQuery.isLoading ? "Calculating..." : "Calculate Projection"}
            </button>
          </div>

          {/* Results */}
          <div className="space-y-6">
            {projectionQuery.data && (
              <>
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
                        labelFormatter={(month: number) => `Month ${month}`}
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
              </>
            )}

            {!projectionQuery.data && !projectionQuery.isLoading && (
              <div className="flex flex-col items-center justify-center py-12 text-center">
                <div className="flex h-16 w-16 items-center justify-center rounded-full bg-black/[0.04] mb-4">
                  <TrendingUp className="h-8 w-8 text-black/40" />
                </div>
                <h4 className="text-base font-medium text-black mb-2">
                  Ready to Calculate
                </h4>
                <p className="text-sm text-black/60">
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