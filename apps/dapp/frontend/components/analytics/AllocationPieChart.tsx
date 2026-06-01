import { PieChart, Pie, Cell, Tooltip, Legend, ResponsiveContainer } from "recharts";

interface AllocationItem {
  protocol: string;
  allocation_pct: number;
  balance_usd: number;
  apy: number;
}

interface AllocationPieChartProps {
  data: AllocationItem[];
}

export default function AllocationPieChart({ data }: AllocationPieChartProps) {
  if (!data || data.length === 0) {
    return <div className="h-[300px] flex items-center justify-center text-gray-500">No allocation data available</div>;
  }

  const chartData = data.map(item => ({
    name: item.protocol,
    value: item.allocation_pct,
    balance: item.balance_usd,
    apy: item.apy,
  }));

  const COLORS = ['#8884d8', '#82ca9d', '#ffc658', '#ff8042', '#008B8B', '#B8860B', '#DA70D6'];

  const formatTooltipLabel = (label: unknown) => {
    const item = chartData.find(d => d.name === label);
    const balance = item?.balance?.toLocaleString(undefined, { style: 'currency', currency: 'USD' });
    const apy = item?.apy?.toFixed(2);
    return `${label} (${balance} | APY: ${apy}%)`;
  };

  return (
    <div className="w-full">
      <ResponsiveContainer width="100%" height={300}>
        <PieChart>
          <Pie
            data={chartData}
            dataKey="value"
            cx="50%"
            cy="50%"
            labelLine={false}
            label={(props) => {
              const { name, value } = props;
              return `${name} ${value}%`;
            }}
          >
            {chartData.map((entry, index) => (
              <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
            ))}
          </Pie>
          <Tooltip
            formatter={(value, name) => `${value}%`}
            contentStyle={{ maxWidth: 200 }}
            labelFormatter={formatTooltipLabel}
          />
          <Legend verticalAlign="bottom" height={36} />
        </PieChart>
      </ResponsiveContainer>
    </div>
  );
}
export { AllocationPieChart };