import { PieChart, Pie, Cell } from "recharts";

interface RiskFactor {
  name: string;
  score: number;
  weight: number;
  reason: string;
  available: boolean;
  confidence: number;
}

interface RiskGaugeChartProps {
  data: {
    overall: number;
    confidence: number;
    tier: string;
    factors: RiskFactor[];
  };
}

const RiskGaugeChart = ({ data }: RiskGaugeChartProps) => {
  const getColor = (score: number): string => {
    if (score >= 0 && score <= 33) {
      return "#10b981";
    } else if (score >= 34 && score <= 66) {
      return "#f59e0b";
    } else {
      return "#ef4444";
    }
  };

  const confidencePercent = Math.round(data.confidence * 100);

  return (
    <div className="flex flex-col items-center gap-4">
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
      <div className="text-sm text-muted-foreground">
        Confidence: {confidencePercent}%
      </div>
    </div>
  );
};

interface RiskDimensionsTableProps {
  data: {
    overall: number;
    confidence: number;
    tier: string;
    factors: RiskFactor[];
  };
}

const RiskDimensionsTable = ({ data }: RiskDimensionsTableProps) => {
  const formatFactorName = (name: string): string => {
    return name
      .split("_")
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(" ");
  };

  const getScoreColor = (score: number): string => {
    if (score >= 0 && score <= 33) {
      return "text-green-600";
    } else if (score >= 34 && score <= 66) {
      return "text-amber-600";
    } else {
      return "text-red-600";
    }
  };

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b">
            <th className="p-3 text-left text-xs font-medium text-gray-500 uppercase">
              Factor
            </th>
            <th className="p-3 text-left text-xs font-medium text-gray-500 uppercase">
              Score
            </th>
            <th className="p-3 text-left text-xs font-medium text-gray-500 uppercase">
              Weight
            </th>
            <th className="p-3 text-left text-xs font-medium text-gray-500 uppercase">
              Status
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200">
          {data.factors.map((factor, index) => (
            <tr key={index} className="hover:bg-gray-50 dark:hover:bg-gray-900">
              <td className="p-3">
                <div className="flex flex-col">
                  <span className="font-medium">{formatFactorName(factor.name)}</span>
                  <span className="text-xs text-muted-foreground">{factor.reason}</span>
                </div>
              </td>
              <td className={`p-3 text-left font-medium ${factor.available ? getScoreColor(factor.score) : "text-gray-400"}`}>
                {factor.available ? factor.score.toFixed(1) : "N/A"}
              </td>
              <td className="p-3 text-left">
                {(factor.weight * 100).toFixed(0)}%
              </td>
              <td className="p-3 text-left">
                {factor.available ? (
                  <span className="text-xs px-2 py-1 rounded bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">
                    Available
                  </span>
                ) : (
                  <span className="text-xs px-2 py-1 rounded bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400">
                    Unavailable
                  </span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export { RiskGaugeChart, RiskDimensionsTable };