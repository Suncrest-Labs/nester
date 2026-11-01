"use client";

import { useMemo } from "react";
import Link from "next/link";
import { ExternalLink, Loader2, RefreshCw } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useYieldHarvests } from "@/hooks/useYieldHarvests";
import { getExplorerTxUrl } from "@/utils/explorer";
import type { ListYieldHarvestsResponse } from "@/lib/api/yields";
import { LoadingRegion } from "@/components/ui/skeleton/skeleton";
import {
  BigNumber,
  BUTTON_SECONDARY,
  EmptyPanel,
  PageHeader,
  Panel,
  Section,
  SKELETON,
} from "@/components/ui/page-kit";
import { cn } from "@/lib/utils";

const TH = "px-6 py-3 text-[13px] font-medium text-muted-foreground";
const TD = "px-6 py-4 text-sm text-foreground";

function formatAmount(value: number) {
  return value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 6 });
}

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export default function YieldHarvestHistoryPage() {
  const {
    data,
    isLoading,
    isError,
    refetch,
    isFetching,
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

  const currency = allHarvests[0]?.currency ?? "";

  return (
    <AppShell>
      <PageHeader
        title="Harvest history"
        lede={
          <p className="text-sm text-muted-foreground">
            Every yield harvest recorded for your account, with a link to the transaction.
          </p>
        }
        actions={
          <Link href="/yields" className={BUTTON_SECONDARY}>
            Back to markets
          </Link>
        }
      />

      {isLoading ? (
        <Panel className="mt-8">
          <LoadingRegion label="Loading harvest history" className="space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className={cn(SKELETON, "h-12")} />
            ))}
          </LoadingRegion>
        </Panel>
      ) : isError ? (
        <div role="alert" data-testid="harvests-error" className="mt-8">
          <EmptyPanel
            title="Harvest history didn't load."
            action={
              <button
                type="button"
                onClick={() => refetch()}
                disabled={isFetching}
                data-testid="harvests-retry"
                className={BUTTON_SECONDARY}
              >
                <RefreshCw
                  className={cn("h-3 w-3", isFetching && "animate-spin")}
                  aria-hidden="true"
                />
                {isFetching ? "Retrying…" : "Retry"}
              </button>
            }
          >
            We couldn&apos;t reach the server. Try again in a moment.
          </EmptyPanel>
        </div>
      ) : allHarvests.length === 0 ? (
        <div data-testid="harvests-empty-state" className="mt-8">
          <EmptyPanel title="No harvests yet">
            Harvested yield will appear here once it has been recorded.
          </EmptyPanel>
        </div>
      ) : (
        <>
          <Panel className="mt-8">
            <h2 className="text-sm font-medium text-muted-foreground">Total harvested</h2>
            <BigNumber className="mt-3 text-5xl">
              {formatAmount(totalEarned)}
              {currency && <span className="ml-2 text-2xl text-muted-foreground">{currency}</span>}
            </BigNumber>
          </Panel>

          <Section id="harvests-heading" title="Harvests">
            <Panel className="overflow-hidden p-0">
              <div className="overflow-x-auto">
                <table className="w-full min-w-[600px] border-collapse text-left">
                  <thead>
                    <tr className="border-b">
                      <th scope="col" className={TH}>Date</th>
                      <th scope="col" className={TH}>Protocol</th>
                      <th scope="col" className={cn(TH, "text-right")}>Amount</th>
                      <th scope="col" className={TH}>Currency</th>
                      <th scope="col" className={TH}>Transaction</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {allHarvests.map((harvest) => (
                      <tr key={harvest.id} className="transition-colors hover:bg-foreground/5">
                        <td className={TD}>{formatDate(harvest.harvested_at)}</td>
                        <td className={cn(TD, "font-medium")}>{harvest.protocol || "N/A"}</td>
                        <td className={cn(TD, "text-right tabular-nums")}>
                          +{formatAmount(parseFloat(harvest.amount))}
                        </td>
                        <td className={TD}>{harvest.currency}</td>
                        <td className={TD}>
                          {harvest.tx_hash ? (
                            <a
                              href={getExplorerTxUrl(harvest.tx_hash)}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center gap-1 text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
                            >
                              View on explorer
                              <ExternalLink className="h-3 w-3" aria-hidden="true" />
                            </a>
                          ) : (
                            <span className="text-muted-foreground">N/A</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {hasNextPage && (
                <div className="flex justify-center border-t px-6 py-4">
                  <button
                    type="button"
                    onClick={() => fetchNextPage()}
                    disabled={isFetchingNextPage}
                    className={BUTTON_SECONDARY}
                  >
                    {isFetchingNextPage && (
                      <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
                    )}
                    Load more
                  </button>
                </div>
              )}
            </Panel>
          </Section>
        </>
      )}
    </AppShell>
  );
}
