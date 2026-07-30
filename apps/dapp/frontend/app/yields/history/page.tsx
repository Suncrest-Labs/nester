"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { useYieldHarvests } from "@/hooks/useYieldHarvests";
import { EmptyState } from "@/components/ui/empty-state/empty-state";
import { getExplorerTxUrl } from "@/utils/explorer";
import { ArrowLeft, Calendar, ExternalLink, Loader2 } from "lucide-react";
import Link from "next/link";
import type { ListYieldHarvestsResponse } from "@/lib/api/yields";

export default function YieldHarvestHistoryPage() {
  const router = useRouter();
  const {
    data,
    isLoading,
    isError,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useYieldHarvests();

  const allHarvests = useMemo(() => {
    return data?.pages.flatMap((page: ListYieldHarvestsResponse) => page.items) ?? [];
  }, [data]);

  const totalEarned = useMemo(() => {
    return allHarvests.reduce((sum, harvest) => sum + parseFloat(harvest.amount), 0);
  }, [allHarvests]);

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  return (
    <AppShell>
      <div className="mx-auto max-w-5xl px-5 py-8 sm:px-8">
        <div className="mb-6">
          <button
            onClick={() => router.back()}
            className="flex items-center gap-1.5 text-xs text-black/50 dark:text-white/50 hover:text-black dark:hover:text-white transition-colors mb-3"
          >
            <ArrowLeft className="h-3 w-3" />
            Back
          </button>
          <h1 className="text-2xl font-semibold text-black dark:text-white tracking-tight">
            Harvest History
          </h1>
          <p className="mt-1 text-sm text-black/55 dark:text-white/55">
            View all your past yield harvests.
          </p>
        </div>

        {isLoading ? (
          <div className="flex flex-col items-center justify-center rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] py-16">
            <Loader2 className="h-8 w-8 animate-spin text-black/30 dark:text-white/30" />
            <p className="mt-4 text-sm text-black/50 dark:text-white/50">Loading harvests...</p>
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center justify-center rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] py-16">
            <p className="text-sm text-black/50 dark:text-white/50">Failed to load harvest history.</p>
          </div>
        ) : allHarvests.length === 0 ? (
          <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F]">
            <EmptyState
              icon={Calendar}
              title="No harvests yet"
              description="Once you start harvesting yield from your vaults, they'll appear here."
            />
          </div>
        ) : (
          <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse min-w-[600px]">
                <thead>
                  <tr className="border-b border-black/8 dark:border-white/8 bg-black/[0.02] dark:bg-white/[0.02]">
                    <th className="px-6 py-3.5 text-[10px] font-medium text-black/50 dark:text-white/50 uppercase tracking-wider">
                      Date
                    </th>
                    <th className="px-6 py-3.5 text-[10px] font-medium text-black/50 dark:text-white/50 uppercase tracking-wider">
                      Protocol
                    </th>
                    <th className="px-6 py-3.5 text-[10px] font-medium text-black/50 dark:text-white/50 uppercase tracking-wider text-right">
                      Amount
                    </th>
                    <th className="px-6 py-3.5 text-[10px] font-medium text-black/50 dark:text-white/50 uppercase tracking-wider">
                      Currency
                    </th>
                    <th className="px-6 py-3.5 text-[10px] font-medium text-black/50 dark:text-white/50 uppercase tracking-wider">
                      Transaction
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-black/8">
                  {allHarvests.map((harvest) => (
                    <tr key={harvest.id} className="hover:bg-black/[0.02] dark:hover:bg-white/[0.02] transition-colors">
                      <td className="px-6 py-5">
                        <span className="text-sm text-black dark:text-white">{formatDate(harvest.harvested_at)}</span>
                      </td>
                      <td className="px-6 py-5">
                        <span className="text-sm font-medium text-black dark:text-white">
                          {harvest.protocol || "N/A"}
                        </span>
                      </td>
                      <td className="px-6 py-5 text-right">
                        <span className="text-sm font-mono text-emerald-600">
                          +{parseFloat(harvest.amount).toLocaleString(undefined, {
                            minimumFractionDigits: 2,
                            maximumFractionDigits: 6,
                          })}
                        </span>
                      </td>
                      <td className="px-6 py-5">
                        <span className="text-sm text-black dark:text-white">{harvest.currency}</span>
                      </td>
                      <td className="px-6 py-5">
                        {harvest.tx_hash ? (
                          <a
                            href={getExplorerTxUrl(harvest.tx_hash)}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-xs font-medium text-black/50 dark:text-white/50 hover:text-black dark:hover:text-white transition-colors"
                          >
                            View on Explorer
                            <ExternalLink className="h-3 w-3" />
                          </a>
                        ) : (
                          <span className="text-xs text-black/30 dark:text-white/30">N/A</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr className="border-t border-black/8 dark:border-white/8 bg-black/[0.02] dark:bg-white/[0.02]">
                    <td
                      colSpan={2}
                      className="px-6 py-4 text-sm font-semibold text-black dark:text-white"
                    >
                      Total Earned
                    </td>
                    <td className="px-6 py-4 text-right text-sm font-mono font-semibold text-emerald-600">
                      +{totalEarned.toLocaleString(undefined, {
                        minimumFractionDigits: 2,
                        maximumFractionDigits: 6,
                      })}
                    </td>
                    <td className="px-6 py-4 text-sm font-medium text-black dark:text-white">
                      {allHarvests[0]?.currency || ""}
                    </td>
                    <td></td>
                  </tr>
                </tfoot>
              </table>
            </div>
            {hasNextPage && (
              <div className="px-6 py-4 border-t border-black/8 dark:border-white/8 flex justify-center">
                <button
                  onClick={() => fetchNextPage()}
                  disabled={isFetchingNextPage}
                  className="flex items-center gap-2 rounded-lg border border-black/10 dark:border-white/10 px-4 py-2 text-xs font-medium text-black dark:text-white hover:bg-black/[0.02] dark:hover:bg-white/[0.02] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isFetchingNextPage ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : null}
                  Load More
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </AppShell>
  );
}
