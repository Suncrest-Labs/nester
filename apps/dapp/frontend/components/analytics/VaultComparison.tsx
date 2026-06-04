"use client";

import { useState, useMemo } from "react";

interface Vault {
  id: string;
  name: string;
  balance_usd: number;
  apy: number;
  yield_earned: number;
  lock_period_days: number;
}

interface VaultComparisonProps {
  vaults: Vault[];
}

export default function VaultComparison({ vaults }: VaultComparisonProps) {
  const [sortKey, setSortKey] = useState<keyof Vault>("apy");
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("desc");

  const sortedVaults = useMemo(() => {
    return [...vaults].sort((a, b) => {
      if (a[sortKey] < b[sortKey]) return sortOrder === "asc" ? -1 : 1;
      if (a[sortKey] > b[sortKey]) return sortOrder === "asc" ? 1 : -1;
      return 0;
    });
  }, [vaults, sortKey, sortOrder]);

  const handleSort = (key: keyof Vault) => {
    if (sortKey === key) {
      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
    } else {
      setSortKey(key);
      setSortOrder("desc");
    }
  };

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse">
        <thead>
          <tr className="bg-gray-50">
            <th
              onClick={() => handleSort("name")}
              className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider cursor-pointer"
            >
              Vault {sortKey === "name" ? (sortOrder === "asc" ? " ▲" : " ▼") : ""}
            </th>
            <th
              onClick={() => handleSort("balance_usd")}
              className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider cursor-pointer"
            >
              Balance {sortKey === "balance_usd" ? (sortOrder === "asc" ? " ▲" : " ▼") : ""}
            </th>
            <th
              onClick={() => handleSort("apy")}
              className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider cursor-pointer"
            >
              APY {sortKey === "apy" ? (sortOrder === "asc" ? " ▲" : " ▼") : ""}
            </th>
            <th
              onClick={() => handleSort("yield_earned")}
              className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider cursor-pointer"
            >
              Yield Earned {sortKey === "yield_earned" ? (sortOrder === "asc" ? " ▲" : " ▼") : ""}
            </th>
            <th
              onClick={() => handleSort("lock_period_days")}
              className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider cursor-pointer"
            >
              Lock Period {sortKey === "lock_period_days" ? (sortOrder === "asc" ? " ▲" : " ▼") : ""}
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200">
          {sortedVaults.map((vault) => (
            <tr key={vault.id} className="bg-white hover:bg-gray-50">
              <td className="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">
                {vault.name}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                ${vault.balance_usd.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                {vault.apy.toFixed(2)}%
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                ${vault.yield_earned.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                {vault.lock_period_days} days
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
export { VaultComparison };