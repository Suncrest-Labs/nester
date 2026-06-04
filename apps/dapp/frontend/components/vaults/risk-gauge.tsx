import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { RiskGaugeChart, RiskDimensionsTable } from "./risk-components";

interface RiskData {
  overall: number;
  tier: string;
  concentration_risk: number;
  protocol_risk: number;
  yield_volatility: number;
  liquidity_risk: number;
}

interface RiskGaugeProps {
  vaultId: string;
}

export default function RiskGauge({ vaultId }: RiskGaugeProps) {
  const [riskData, setRiskData] = useState<RiskData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  useEffect(() => {
    const fetchRiskData = async () => {
      setLoading(true);
      setError(null);
      try {
        const response = await fetch(`/api/v1/vaults/${vaultId}/risk`);
        if (!response.ok) {
          if (response.status === 400) {
            const errorData = await response.json();
            throw new Error(errorData.error?.message || "Invalid vault");
          } else {
            throw new Error("Failed to fetch risk data");
          }
        }
        const data = await response.json();
        setRiskData(normalizeRiskData(data));
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unknown error");
      } finally {
        setLoading(false);
      }
    };

    fetchRiskData();
  }, [vaultId, router]);

  if (loading) {
    return (
      <div className="h-[200px] flex items-center justify-center text-gray-500">
        Loading risk data...
      </div>
    );
  }

  if (error) {
    return (
      <div className="h-[200px] flex items-center justify-center text-red-500">
        {error}
      </div>
    );
  }

  if (!riskData) {
    return (
      <div className="h-[200px] flex items-center justify-center text-gray-500">
        No risk data available
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="border rounded-xl p-6">
        <h3 className="text-lg font-semibold mb-4">Vault Risk Assessment</h3>
        <RiskGaugeChart data={riskData} />
      </div>
      
      <div className="border rounded-xl p-6">
        <h3 className="text-lg font-semibold mb-4">Risk Dimension Breakdown</h3>
        <RiskDimensionsTable data={riskData} />
      </div>
    </div>
  );
}
export { RiskGauge };

function normalizeRiskData(data: Record<string, unknown>): RiskData {
  const dimensions = Array.isArray(data?.dimensions) ? (data.dimensions as Array<{name: string; score: number}>) : [];
  const scoreFor = (name: string) =>
    dimensions.find((dimension: {name: string; score: number}) =>
      String(dimension?.name ?? "").toLowerCase().includes(name)
    )?.score ?? 0;

  const getNumber = (value: unknown, fallback: unknown = 0): number => {
    if (typeof value === "number") return value;
    if (typeof fallback === "number") return fallback;
    return 0;
  };

  const getString = (value: unknown, fallback: string = "Unknown"): string => {
    if (typeof value === "string") return value;
    return fallback;
  };

  return {
    overall: getNumber(data?.overall, data?.score),
    tier: getString(data?.tier, getString(data?.level, "Unknown")),
    concentration_risk: data?.concentration_risk ? getNumber(data.concentration_risk) : scoreFor("concentration"),
    protocol_risk: data?.protocol_risk ? getNumber(data.protocol_risk) : scoreFor("protocol"),
    yield_volatility: data?.yield_volatility ? getNumber(data.yield_volatility) : scoreFor("yield"),
    liquidity_risk: data?.liquidity_risk ? getNumber(data.liquidity_risk) : scoreFor("liquidity"),
  };
}
