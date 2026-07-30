"use client";

import { BarChart, Bar, XAxis, YAxis, Tooltip, Legend, ResponsiveContainer } from "recharts";

interface BenchmarkCardProps {
  userAPY: number;
  defiLlamaAPY: number;
  nigeriaBankRate: number;
  nigeriaInflationRate: number;
}

export default function BenchmarkCard({ 
  userAPY, 
  defiLlamaAPY, 
  nigeriaBankRate, 
  nigeriaInflationRate 
}: BenchmarkCardProps) {
  // If all values are zero or undefined, show empty state
  if ([userAPY, defiLlamaAPY, nigeriaBankRate, nigeriaInflationRate].every(v => v === 0 || v === undefined)) {
    return <div className="h-[300px] flex items-center justify-center text-gray-500">No benchmark data available</div>;
  }

  const chartData = [
    { name: 'User', apy: userAPY },
    { name: 'DeFi Avg', apy: defiLlamaAPY },
    { name: 'Bank (NG)', apy: nigeriaBankRate },
    { name: 'Inflation (NG)', apy: nigeriaInflationRate }
  ];

  const COLORS = ['#82ca9d', '#8884d8', '#ffc658', '#ff8042'];

  return (
    <div className="w-full space-y-6">
      <div className="w-full">
        <ResponsiveContainer width="100%" height={300}>
          <BarChart data={chartData} margin={{ top: 20, right: 30, left: 0, bottom: 5 }}>
            <XAxis dataKey="name" tick={{ fontSize: 12 }} />
            <YAxis tickFormatter={(value) => `${value}%`} tick={{ fontSize: 12 }} />
            <Tooltip />
            <Legend verticalAlign="top" height={36} />
            {chartData.map((entry, index) => (
              <Bar 
                key={`bar-${index}`} 
                dataKey="apy" 
                fill={COLORS[index % COLORS.length]} 
                radius={[4, 4, 0, 0]}
              />
            ))}
          </BarChart>
        </ResponsiveContainer>
      </div>
      <div className="text-xs text-gray-500">
        Source: CBN (bank rate), NBS (inflation), DefiLlama (DeFi avg)
      </div>
    </div>
  );
}
export { BenchmarkCard };