import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { RiskGaugeChart, RiskDimensionsTable } from "./risk-components";

interface RiskGaugeProps {
  vaultId: string;
}

export default function RiskGauge({ vaultId }: RiskGaugeProps) {
  const [riskData, setRiskData] = useState<any>(null);
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
        setRiskData(data);
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