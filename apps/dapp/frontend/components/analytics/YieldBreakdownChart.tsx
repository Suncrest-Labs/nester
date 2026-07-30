"use client";

import { BarChart, Bar, XAxis, YAxis, Tooltip, Legend, ResponsiveContainer } from "recharts";

interface VaultMonthlyYield {
  vault_id: string;
  vault_name: string;
  month: string; // format: "YYYY-MM"
  yield_usd: number;
}

interface YieldBreakdownChartProps {
  data: VaultMonthlyYield[];
}

export default function YieldBreakdownChart({ data }: YieldBreakdownChartProps) {
  if (!data || data.length === 0) {
    return <div className="h-[300px] flex items-center justify-center text-gray-500">No yield breakdown data available</div>;
  }

  // Group by month
  const monthsMap = new Map<string, Map<string, number>>();
  const vaultNames = new Set<string>();

  data.forEach(item => {
    if (!monthsMap.has(item.month)) {
      monthsMap.set(item.month, new Map<string, number>());
    }
    const vaultMap = monthsMap.get(item.month)!;
    vaultMap.set(item.vault_name, item.yield_usd);
    vaultNames.add(item.vault_name);
  });

  // Sort months chronologically
  const sortedMonths = Array.from(monthsMap.keys()).sort((a, b) => {
    const [yearA, monthA] = a.split('-').map(Number);
    const [yearB, monthB] = b.split('-').map(Number);
    return yearA - yearB || monthA - monthB;
  });

  // Prepare data for Recharts: each month becomes an object with properties for each vault
  const chartData = sortedMonths.map(month => {
    const monthObj: Record<string, string | number> = { month };
    const vaultMap = monthsMap.get(month)!;
    vaultNames.forEach(vault => {
      monthObj[vault] = vaultMap.get(vault) || 0;
    });
    return monthObj;
  });

  // Get the list of vault names for the stack
  const stackKeys = Array.from(vaultNames);

  // Define colors for the vaults (we'll use a simple color array, in practice you might want a color scale)
  const COLORS = ['#8884d8', '#82ca9d', '#ffc658', '#ff8042', '#008B8B', '#B8860B', '#DA70D6'];

  return (
    <div className="w-full">
      <ResponsiveContainer width="100%" height={300}>
        <BarChart data={chartData} margin={{ top: 20, right: 30, left: 0, bottom: 5 }}>
          <XAxis dataKey="month" tick={{ fontSize: 12 }} />
          <YAxis tickFormatter={(value) => `$${value}`} tick={{ fontSize: 12 }} />
          <Tooltip />
          <Legend verticalAlign="top" height={36} />
          {stackKeys.map((key, index) => (
            <Bar
              key={`bar-${key}`}
              dataKey={key}
              stackId="a"
              fill={COLORS[index % COLORS.length]}
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
export { YieldBreakdownChart };