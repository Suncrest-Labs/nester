import { PieChart, Pie, Cell, Tooltip, Legend } from "recharts";

interface RiskGaugeChartProps {
  data: {
    overall: number;
    tier: string;
    concentration_risk: number;
    protocol_risk: number;
    yield_volatility: number;
    liquidity_risk: number;
  };
}

const RiskGaugeChart = ({ data }: RiskGaugeChartProps) => {
  const getColor = (score: number): string => {
    if (score >= 0 && score <= 33) {
      return "#10b981"; // green-500
    } else if (score >= 34 && score <= 66) {
      return "#f59e0b"; // amber-500
    } else {
      return "#ef4444"; // red-500
    }
  };

  return (
    <div className="relative w-[100px] h-[100px] mx-auto">
      <PieChart className="w-full h-full">
        <Pie
          data={[
            { name: "score", value: data.overall },
            { name: "background", value: 100 - data.overall },
          ]}
          dataKey="value"
          nameKey="name"
          cx="50%"
          cy="50%"
          innerRadius={60}
          outerRadius={80}
        >
          {[
            { name: "score", value: data.overall },
            { name: "background", value: 100 - data.overall },
          ].map((entry, index) => (
            <Cell
              key={`cell-${index}`}
              fill={entry.name === "score" ? getColor(data.overall) : "#e5e7eb"}
            />
          ))}
        </Pie>
      </PieChart>
      <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
        <div className="text-2xl font-bold">{Math.round(data.overall)}</div>
        <div className="text-sm text-gray-500">{data.tier}</div>
      </div>
    </div>
  );
};

interface RiskDimensionsTableProps {
  data: {
    overall: number;
    tier: string;
    concentration_risk: number;
    protocol_risk: number;
    yield_volatility: number;
    liquidity_risk: number;
  };
}

const RiskDimensionsTable = ({ data }: RiskDimensionsTableProps) => {
  const dimensions = [
    {
      name: "Concentration",
      score: data.concentration_risk,
      weight: "35%",
      explanation:
        "Measures how concentrated your vault is across different protocols. Higher concentration means higher risk.",
    },
    {
      name: "Protocol Risk",
      score: data.protocol_risk,
      weight: "30%",
      explanation:
        "Based on the inherent risk of the protocols you are invested in (Aave, Blend, Compound).",
    },
    {
      name: "Yield Volatility",
      score: data.yield_volatility,
      weight: "20%",
      explanation:
        "Measures the variability of your vault's APY over time. Higher volatility means higher risk.",
    },
    {
      name: "Liquidity Risk",
      score: data.liquidity_risk,
      weight: "15%",
      explanation:
        "Measures the size of your vault relative to the total market size of the protocol. Higher ratio means higher risk.",
    },
  ];

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b">
            <th className="p-3 text-left text-xs font-medium text-gray-500 uppercase">
              Dimension
            </th>
            <th className="p-3 text-left text-xs font-medium text-gray-500 uppercase">
              Score
            </th>
            <th className="p-3 text-left text-xs font-medium text-gray-500 uppercase">
              Weight
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200">
          {dimensions.map((dimension, index) => (
            <tr key={index} className="hover:bg-gray-50">
              <td className="p-3 flex items-center">
                <span className="mr-2">{dimension.name}</span>
                <div className="relative inline-block">
                  <button
                    onMouseEnter={() => {}} // Tooltip handled in parent component
                    onMouseLeave={() => {}}
                    className="text-xs text-gray-400 hover:text-gray-600"
                  >
                    (?)
                  </button>
                </div>
              </td>
              <td className="p-3 text-left">{dimension.score.toFixed(1)}</td>
              <td className="p-3 text-left">{dimension.weight}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export { RiskGaugeChart, RiskDimensionsTable };