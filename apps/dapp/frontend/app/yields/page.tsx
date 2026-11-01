"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { ArrowDown, ArrowUp, Bookmark, BookmarkCheck, RefreshCw } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { LoadingRegion } from "@/components/ui/skeleton/skeleton";
import {
  BigNumber,
  BUTTON_PRIMARY,
  BUTTON_SECONDARY,
  EmptyPanel,
  PageHeader,
  PANEL,
  Panel,
  Section,
  SegmentedControl,
  SKELETON,
} from "@/components/ui/page-kit";
import { useWallet } from "@/components/wallet-provider";
import { useYields } from "@/hooks/useYields";
import { useSavings } from "@/hooks/useSavings";
import { formatPercent } from "@/components/savings-rate/format";
import {
  addYieldBookmark,
  fetchYieldBookmarks,
  protocolSlugFromProject,
  removeYieldBookmark,
  type YieldPool,
} from "@/lib/api/yields";
import { getStoredToken } from "@/lib/api/client";
import { cn } from "@/lib/utils";

type RiskTier = "all" | "low" | "medium" | "high";
type SortKey = "apy" | "tvl";
type SortDir = "asc" | "desc";
type SortState = { key: SortKey; dir: SortDir };

const RISK_TABS: { id: RiskTier; label: string }[] = [
  { id: "all", label: "All" },
  { id: "low", label: "Low" },
  { id: "medium", label: "Medium" },
  { id: "high", label: "High" },
];

const RISK_STYLE: Record<Exclude<RiskTier, "all">, { label: string; dot: string }> = {
  low: { label: "Low", dot: "bg-emerald-500" },
  medium: { label: "Medium", dot: "bg-amber-500" },
  high: { label: "High", dot: "bg-red-500" },
};

const ROW_GRID =
  "grid grid-cols-2 gap-x-4 gap-y-3 md:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_2rem] md:items-center md:gap-y-0";
const NO_BOOKMARKS: ReadonlySet<string> = new Set();
const MOBILE_LABEL = "block text-xs text-muted-foreground md:hidden";

function riskTier(score: number): Exclude<RiskTier, "all"> {
  if (score <= 25) return "low";
  if (score <= 55) return "medium";
  return "high";
}

function fmtTVL(usd: number): string {
  if (usd >= 1_000_000_000) return `$${(usd / 1_000_000_000).toFixed(1)}B`;
  if (usd >= 1_000_000) return `$${(usd / 1_000_000).toFixed(1)}M`;
  if (usd >= 1_000) return `$${(usd / 1_000).toFixed(0)}K`;
  return `$${usd.toFixed(0)}`;
}

function SavingsComparison() {
  const { summary, isLoading } = useSavings();

  return (
    <section
      data-testid="savings-rate-cta"
      aria-labelledby="savings-rate-compare"
      className={cn(PANEL, "mt-8 flex flex-wrap items-end justify-between gap-6")}
    >
      <div className="max-w-[60ch]">
        <h2 id="savings-rate-compare" className="text-sm font-medium text-muted-foreground">
          Nester Savings Rate
        </h2>
        {summary ? (
          summary.rate7d === null ? (
            <p className="mt-3 font-heading text-3xl font-light text-muted-foreground">
              Not enough history yet
            </p>
          ) : (
            <>
              <BigNumber className="mt-3 text-5xl">{formatPercent(summary.rate7d, 2)}</BigNumber>
              <p className="mt-2 text-sm text-muted-foreground">
                Realized rate, last 7 days. Not guaranteed.
              </p>
            </>
          )
        ) : (
          isLoading && <div className={cn(SKELETON, "mt-3 h-12 w-40")} aria-hidden="true" />
        )}
        <p className="mt-4 text-sm text-foreground/80">
          The markets below are for comparison, not where your deposit goes. The Savings Rate
          allocates to Blend and DeFindex, and the dashboard shows exactly where your money sits.
        </p>
      </div>
      <Link href="/dashboard" className={BUTTON_PRIMARY}>
        Save with Nester
      </Link>
    </section>
  );
}

function RiskLabel({ score }: { score: number }) {
  const risk = RISK_STYLE[riskTier(score)];
  return (
    <span className="inline-flex items-center gap-2 text-sm text-foreground">
      <span className={cn("h-1.5 w-1.5 rounded-full", risk.dot)} aria-hidden="true" />
      {risk.label}
    </span>
  );
}

function SortHeader({
  label,
  sortKey,
  sort,
  onSort,
}: {
  label: string;
  sortKey: SortKey;
  sort: SortState;
  onSort: (key: SortKey) => void;
}) {
  const active = sort.key === sortKey;
  const Icon = sort.dir === "asc" ? ArrowUp : ArrowDown;
  return (
    <div
      role="columnheader"
      aria-sort={active ? (sort.dir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        className={cn(
          "inline-flex items-center gap-1 rounded transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground",
          active && "text-foreground"
        )}
      >
        {label}
        {active && <Icon className="h-3 w-3" aria-hidden="true" />}
      </button>
    </div>
  );
}

function MarketRow({
  pool,
  bookmarked,
  onBookmark,
}: {
  pool: YieldPool;
  bookmarked: boolean;
  onBookmark: (pool: YieldPool) => void;
}) {
  return (
    <div
      role="row"
      data-testid="yield-card"
      className={cn(ROW_GRID, "px-6 py-4 transition-colors hover:bg-foreground/5")}
    >
      <div role="cell" className="order-1 min-w-0 md:order-none">
        <span className="block truncate text-sm font-medium capitalize text-foreground">
          {pool.project}
        </span>
      </div>
      <div role="cell" className="order-3 md:order-none">
        <span className={MOBILE_LABEL}>Asset</span>
        <span className="text-sm text-foreground">{pool.symbol}</span>
      </div>
      <div role="cell" className="order-4 md:order-none">
        <span className={MOBILE_LABEL}>APY</span>
        <span className="text-sm font-medium tabular-nums text-foreground">
          {pool.apy.toFixed(2)}%
        </span>
      </div>
      <div role="cell" className="order-5 md:order-none">
        <span className={MOBILE_LABEL}>TVL</span>
        <span className="text-sm tabular-nums text-foreground">{fmtTVL(pool.tvlUsd)}</span>
      </div>
      <div role="cell" className="order-6 md:order-none">
        <span className={MOBILE_LABEL}>Risk</span>
        <RiskLabel score={pool.riskScore} />
      </div>
      <div role="cell" className="order-2 justify-self-end md:order-none">
        <button
          type="button"
          onClick={() => onBookmark(pool)}
          aria-label={bookmarked ? "Remove bookmark" : "Bookmark protocol"}
          className="flex h-8 w-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground"
        >
          {bookmarked ? (
            <BookmarkCheck className="h-4 w-4 text-foreground" />
          ) : (
            <Bookmark className="h-4 w-4" />
          )}
        </button>
      </div>
    </div>
  );
}

export default function YieldsPage() {
  const { isConnected } = useWallet();
  const { data, isLoading, isError, refetch, isFetching } = useYields();
  const [riskFilter, setRiskFilter] = useState<RiskTier>("all");
  const [sort, setSort] = useState<SortState>({ key: "apy", dir: "desc" });
  const [bookmarks, setBookmarks] = useState<Set<string>>(new Set());

  const signedIn = isConnected && !!getStoredToken();
  const savedSlugs = signedIn ? bookmarks : NO_BOOKMARKS;

  useEffect(() => {
    if (!signedIn) return;
    fetchYieldBookmarks()
      .then((items) => setBookmarks(new Set(items.map((b) => b.protocol_slug))))
      .catch(() => setBookmarks(new Set()));
  }, [signedIn]);

  const pools = useMemo(() => data?.data ?? [], [data]);
  const isStale = data?.meta?.stale ?? false;

  const rows = useMemo(() => {
    const filtered =
      riskFilter === "all" ? pools : pools.filter((p) => riskTier(p.riskScore) === riskFilter);
    const value = (p: YieldPool) => (sort.key === "apy" ? p.apy : p.tvlUsd);
    const sign = sort.dir === "asc" ? 1 : -1;
    return [...filtered].sort((a, b) => (value(a) - value(b)) * sign);
  }, [pools, riskFilter, sort]);

  const handleSort = useCallback((key: SortKey) => {
    setSort((prev) =>
      prev.key === key ? { key, dir: prev.dir === "desc" ? "asc" : "desc" } : { key, dir: "desc" }
    );
  }, []);

  const handleBookmark = useCallback(
    async (pool: YieldPool) => {
      if (!signedIn) return;
      const slug = protocolSlugFromProject(pool.project);
      const isSaved = savedSlugs.has(slug);
      try {
        if (isSaved) {
          await removeYieldBookmark(slug);
          setBookmarks((prev) => {
            const next = new Set(prev);
            next.delete(slug);
            return next;
          });
        } else {
          await addYieldBookmark(slug);
          setBookmarks((prev) => new Set(prev).add(slug));
        }
      } catch {
        // silently ignore bookmark errors
      }
    },
    [savedSlugs, signedIn]
  );

  return (
    <AppShell>
      <PageHeader
        title="Markets"
        lede={
          <p className="text-sm text-muted-foreground">
            Live DeFi rates on Stellar, shown so you can compare them with the Nester Savings Rate.
          </p>
        }
        actions={
          signedIn ? (
            <Link href="/yields/history" className={BUTTON_SECONDARY}>
              Harvest history
            </Link>
          ) : undefined
        }
      />

      <SavingsComparison />

      <Section id="markets-heading" title="Stellar markets">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <SegmentedControl
            label="Filter by risk"
            options={RISK_TABS}
            value={riskFilter}
            onChange={setRiskFilter}
          />
          {isStale && (
            <p
              className="text-sm text-amber-700 dark:text-amber-400"
              data-testid="yield-stale-indicator"
            >
              Showing cached data. Live rates may differ.
            </p>
          )}
        </div>

        {isLoading ? (
          <Panel>
            <LoadingRegion label="Loading markets" className="space-y-3">
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className={cn(SKELETON, "h-12")} />
              ))}
            </LoadingRegion>
          </Panel>
        ) : isError ? (
          <EmptyPanel
            title="Market rates didn't load."
            action={
              <button
                type="button"
                onClick={() => refetch()}
                disabled={isFetching}
                className={BUTTON_SECONDARY}
              >
                <RefreshCw
                  className={cn("h-3 w-3", isFetching && "animate-spin")}
                  aria-hidden="true"
                />
                Retry
              </button>
            }
          >
            We couldn&apos;t reach the rates service. Try again in a moment.
          </EmptyPanel>
        ) : rows.length === 0 ? (
          <div data-testid="yields-empty-state">
            <EmptyPanel title="No markets to show.">
              {pools.length === 0
                ? "Markets below the minimum TVL threshold are hidden. Check back as liquidity grows."
                : "No markets match this risk level. Try a different filter."}
            </EmptyPanel>
          </div>
        ) : (
          <Panel className="overflow-hidden p-0">
            <div role="table" aria-labelledby="markets-heading">
              <div role="rowgroup" className="hidden border-b md:block">
                <div
                  role="row"
                  className={cn(ROW_GRID, "px-6 py-3 text-[13px] font-medium text-muted-foreground")}
                >
                  <div role="columnheader">Protocol</div>
                  <div role="columnheader">Asset</div>
                  <SortHeader label="APY" sortKey="apy" sort={sort} onSort={handleSort} />
                  <SortHeader label="TVL" sortKey="tvl" sort={sort} onSort={handleSort} />
                  <div role="columnheader">Risk</div>
                  <div role="columnheader">
                    <span className="sr-only">Bookmark</span>
                  </div>
                </div>
              </div>
              <div role="rowgroup" className="divide-y">
                {rows.map((pool) => (
                  <MarketRow
                    key={pool.pool}
                    pool={pool}
                    bookmarked={savedSlugs.has(protocolSlugFromProject(pool.project))}
                    onBookmark={handleBookmark}
                  />
                ))}
              </div>
            </div>
          </Panel>
        )}
      </Section>
    </AppShell>
  );
}
