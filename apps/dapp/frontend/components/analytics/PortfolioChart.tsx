"use client";

import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer, AreaChart, Area } from "recharts";

interface DailySnapshot {
  date: string;
  total_balance_usd: number;
  yield_earned_usd: number;
}

interface PortfolioChartProps {
  data: DailySnapshot[];
}

export default function PortfolioChart({ data }: PortfolioChartProps) {
  if (!data || data.length === 0) {
    return <div className="h-[300px] flex items-center justify-center text-gray-500">No portfolio data available</div>;
  }

  const chartData = data.map(item => ({
    date: item.date,
    total_balance_usd: item.total_balance_usd,
    yield_earned_usd: item.yield_earned_usd,
  }));

  return (
    <div className="w-full">
      <ResponsiveContainer width="100%" height={300}>
        <LineChart data={chartData} margin={{ top: 20, right: 30, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="date" tick={{ fontSize: 12 }} />
          <YAxis tickFormatter={(value) => `$${value}`} tick={{ fontSize: 12 }} />
          <Tooltip />
          <Legend verticalAlign="top" height={36} />
          {/* Area for yield earned */}
          <AreaChart>
            <Area type="monotone" dataKey="yield_earned_usd" stroke="#8884d8" fillOpacity={0.3} />
          </AreaChart>
          {/* Line for total balance */}
          <Line type="monotone" dataKey="total_balance_usd" stroke="#82ca9d" strokeWidth={2} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}