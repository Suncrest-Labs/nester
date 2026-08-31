import { useEffect, useState } from "react";
import { RiskGaugeChart, RiskDimensionsTable } from "./risk-components";

interface RiskFactor {
  name: string;
  score: number;
  weight: number;
  reason: string;
  available: boolean;
  confidence: number;
}

interface RiskData {
  id: string;
  vault_id: string;
  overall: number;
  confidence: number;
  tier: string;
  factors: RiskFactor[];
  computed_at: string;
}

interface RiskGaugeProps {
  vaultId: string;
}

export default function RiskGauge({ vaultId }: RiskGaugeProps) {
  const [riskData, setRiskData] = useState<RiskData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

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
        const result = await response.json();
        const data = result.data || result;
        setRiskData(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unknown error");
      } finally {
        setLoading(false);
      }
    };

    fetchRiskData();
  }, [vaultId]);

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
        <h3 className="text-lg font-semibold mb-4">Risk Factor Breakdown</h3>
        <RiskDimensionsTable data={riskData} />
      </div>
    </div>
  );
}
export { RiskGauge };
