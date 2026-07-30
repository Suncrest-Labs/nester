// apps/dapp/frontend/components/history/FilterBar.tsx

"use client";

import React, { useState, useEffect } from "react";

export type FilterState = {
  fromDate?: string; // ISO date string (YYYY-MM-DD)
  toDate?: string;
  types: Record<string, boolean>; // keys: Deposit, Withdrawal, Rebalance, Settlement, "Yield Earned"
  vaultId?: string;
  status: string; // "All", "Confirmed", "Pending", "Failed"
  searchTerm: string;
};

type Props = {
  initialFilters?: Partial<FilterState>;
  vaultOptions: { id: string; name: string }[];
  onChange: (filters: FilterState) => void;
};

// Helper to initialize type checkboxes
const TRANSACTION_TYPES = [
  "Deposit",
  "Withdrawal",
  "Rebalance",
  "Settlement",
  "Yield Earned",
];

export default function FilterBar({ initialFilters = {}, vaultOptions, onChange }: Props) {
  const [filters, setFilters] = useState<FilterState>({
    fromDate: initialFilters.fromDate,
    toDate: initialFilters.toDate,
    types: TRANSACTION_TYPES.reduce((acc, t) => {
      acc[t] = initialFilters.types?.[t] ?? false;
      return acc;
    }, {} as Record<string, boolean>),
    vaultId: initialFilters.vaultId,
    status: initialFilters.status ?? "All",
    searchTerm: initialFilters.searchTerm ?? "",
  });

  // Communicate changes to parent (debounced for search)
  useEffect(() => {
    const handler = setTimeout(() => onChange(filters), 300);
    return () => clearTimeout(handler);
  }, [filters, onChange]);

  const toggleType = (type: string) => {
    setFilters((prev) => ({
      ...prev,
      types: { ...prev.types, [type]: !prev.types[type] },
    }));
  };

  return (
    <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 p-4 bg-white dark:bg-[#100F0F] rounded-lg shadow-sm">
      {/* Date Range */}
      <div className="flex items-center gap-2">
        <label className="text-sm font-medium text-gray-700">From</label>
        <input
          type="date"
          value={filters.fromDate ?? ""}
          onChange={(e) => setFilters({ ...filters, fromDate: e.target.value })}
          className="border border-gray-300 rounded px-2 py-1 text-sm"
        />
        <label className="text-sm font-medium text-gray-700">To</label>
        <input
          type="date"
          value={filters.toDate ?? ""}
          onChange={(e) => setFilters({ ...filters, toDate: e.target.value })}
          className="border border-gray-300 rounded px-2 py-1 text-sm"
        />
      </div>

      {/* Type Checkboxes */}
      <div className="flex items-center gap-4">
        {TRANSACTION_TYPES.map((type) => (
          <label key={type} className="inline-flex items-center text-sm">
            <input
              type="checkbox"
              checked={filters.types[type] ?? false}
              onChange={() => toggleType(type)}
              className="mr-1"
            />
            {type}
          </label>
        ))}
      </div>

      {/* Vault selector */}
      <div className="flex items-center gap-2">
        <label className="text-sm font-medium text-gray-700">Vault</label>
        <select
          value={filters.vaultId ?? ""}
          onChange={(e) => setFilters({ ...filters, vaultId: e.target.value || undefined })}
          className="border border-gray-300 rounded px-2 py-1 text-sm"
        >
          <option value="">All</option>
          {vaultOptions.map((v) => (
            <option key={v.id} value={v.id}>
              {v.name}
            </option>
          ))}
        </select>
      </div>

      {/* Status selector */}
      <div className="flex items-center gap-2">
        <label className="text-sm font-medium text-gray-700">Status</label>
        <select
          value={filters.status}
          onChange={(e) => setFilters({ ...filters, status: e.target.value })}
          className="border border-gray-300 rounded px-2 py-1 text-sm"
        >
          <option value="All">All</option>
          <option value="Confirmed">Confirmed</option>
          <option value="Pending">Pending</option>
          <option value="Failed">Failed</option>
        </select>
      </div>

      {/* Search */}
      <div className="flex items-center gap-2">
        <input
          type="text"
          placeholder="Search hash or amount"
          value={filters.searchTerm}
          onChange={(e) => setFilters({ ...filters, searchTerm: e.target.value })}
          className="border border-gray-300 rounded px-2 py-1 text-sm w-48"
        />
      </div>
    </div>
  );
}
