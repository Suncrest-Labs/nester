import React, { useMemo } from "react";
import { ProtocolOption, ProtocolAllocation } from "../../lib/types/vault-wizard";
import { AlertCircle, TrendingUp } from "lucide-react";
import clsx from "clsx";

interface AllocationBuilderProps {
  protocols: ProtocolOption[];
  allocations: ProtocolAllocation[];
  onChange: (allocations: ProtocolAllocation[]) => void;
}

export function AllocationBuilder({
  protocols,
  allocations,
  onChange,
}: AllocationBuilderProps) {
  const totalAllocation = useMemo(() => {
    return allocations.reduce((sum, a) => sum + a.percentage, 0);
  }, [allocations]);

  const blendedApy = useMemo(() => {
    let totalApy = 0;
    allocations.forEach((alloc) => {
      const protocol = protocols.find((p) => p.id === alloc.protocolId);
      if (protocol) {
        totalApy += (alloc.percentage / 100) * protocol.estimatedApy;
      }
    });
    return totalApy;
  }, [allocations, protocols]);

  const handleSliderChange = (protocolId: string, newValue: number) => {
    const currentIndex = allocations.findIndex((a) => a.protocolId === protocolId);
    if (currentIndex === -1) return;

    const currentAlloc = allocations[currentIndex];
    const delta = newValue - currentAlloc.percentage;
    
    if (delta === 0) return;

    let newAllocations = [...allocations];
    newAllocations[currentIndex] = { ...currentAlloc, percentage: newValue };

    // Dynamically adjust remaining allocations
    const otherIndices = newAllocations
      .map((_, i) => i)
      .filter((i) => i !== currentIndex);

    const otherTotal = otherIndices.reduce(
      (sum, idx) => sum + allocations[idx].percentage,
      0
    );

    if (otherTotal === 0) {
      // If others are 0, distribute evenly
      const share = -delta / otherIndices.length;
      otherIndices.forEach((idx) => {
        newAllocations[idx].percentage = Math.max(0, newAllocations[idx].percentage + share);
      });
    } else {
      // Distribute proportionally
      otherIndices.forEach((idx) => {
        const proportion = allocations[idx].percentage / otherTotal;
        newAllocations[idx].percentage = Math.max(0, newAllocations[idx].percentage - (delta * proportion));
      });
    }

    // Normalize to handle rounding errors
    const sum = newAllocations.reduce((s, a) => s + a.percentage, 0);
    if (Math.abs(sum - 100) > 0.01) {
      const diff = 100 - sum;
      // Add difference to the largest other allocation
      const largestOtherIdx = [...otherIndices].sort(
        (a, b) => newAllocations[b].percentage - newAllocations[a].percentage
      )[0];
      if (largestOtherIdx !== undefined) {
        newAllocations[largestOtherIdx].percentage += diff;
      }
    }

    // Round to 1 decimal place
    newAllocations = newAllocations.map(a => ({
      ...a,
      percentage: Number(a.percentage.toFixed(1))
    }));

    onChange(newAllocations);
  };

  const getRiskColor = (risk: string) => {
    switch (risk) {
      case "low":
        return "text-green-500 bg-green-500/10";
      case "medium":
        return "text-yellow-500 bg-yellow-500/10";
      case "high":
        return "text-red-500 bg-red-500/10";
      default:
        return "text-gray-500 bg-gray-500/10";
    }
  };

  return (
    <div className="space-y-6">
      {/* Summary Card */}
      <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-6 flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h3 className="text-lg font-medium text-white flex items-center gap-2">
            <TrendingUp className="w-5 h-5 text-blue-400" />
            Estimated Blended APY
          </h3>
          <p className="text-sm text-slate-400 mt-1">
            Weighted average based on your current allocation strategy.
          </p>
        </div>
        <div className="text-3xl font-bold text-transparent bg-clip-text bg-gradient-to-r from-blue-400 to-emerald-400">
          {blendedApy.toFixed(2)}%
        </div>
      </div>

      {/* Warning if total != 100 */}
      {Math.abs(totalAllocation - 100) > 0.1 && (
        <div className="bg-red-500/10 border border-red-500/20 rounded-lg p-4 flex items-start gap-3">
          <AlertCircle className="w-5 h-5 text-red-400 shrink-0 mt-0.5" />
          <div>
            <h4 className="text-sm font-medium text-red-400">Invalid Total Allocation</h4>
            <p className="text-sm text-red-400/80 mt-1">
              Total allocation must be exactly 100%. Current total is {totalAllocation.toFixed(1)}%.
            </p>
          </div>
        </div>
      )}

      {/* Protocol Sliders */}
      <div className="space-y-4">
        {protocols.map((protocol) => {
          const allocation = allocations.find((a) => a.protocolId === protocol.id)?.percentage || 0;
          
          return (
            <div key={protocol.id} className="bg-slate-900 border border-slate-800 rounded-xl p-5 transition-colors hover:border-slate-700">
              <div className="flex justify-between items-start mb-4">
                <div className="flex gap-3">
                  <div 
                    className="w-10 h-10 rounded-full flex items-center justify-center shrink-0"
                    style={{ backgroundColor: protocol.color || '#333' }}
                  >
                    <span className="text-white font-bold text-sm">
                      {protocol.name.charAt(0)}
                    </span>
                  </div>
                  <div>
                    <h4 className="text-white font-medium flex items-center gap-2">
                      {protocol.name}
                      <span className={clsx("text-xs px-2 py-0.5 rounded-full capitalize font-medium", getRiskColor(protocol.riskLevel))}>
                        {protocol.riskLevel} Risk
                      </span>
                    </h4>
                    <p className="text-sm text-slate-400 mt-0.5 max-w-md line-clamp-1">
                      {protocol.description}
                    </p>
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-sm font-medium text-emerald-400">
                    {protocol.estimatedApy.toFixed(1)}% APY
                  </div>
                  <div className="text-xl font-bold text-white mt-1">
                    {allocation.toFixed(1)}%
                  </div>
                </div>
              </div>

              <div className="relative pt-2">
                <input
                  type="range"
                  min="0"
                  max="100"
                  step="0.1"
                  value={allocation}
                  onChange={(e) => handleSliderChange(protocol.id, parseFloat(e.target.value))}
                  className="w-full h-2 bg-slate-800 rounded-lg appearance-none cursor-pointer accent-blue-500"
                />
                <div 
                  className="absolute top-2 left-0 h-2 bg-blue-500 rounded-l-lg pointer-events-none" 
                  style={{ width: `${allocation}%` }}
                />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
