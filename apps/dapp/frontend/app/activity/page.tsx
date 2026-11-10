"use client";

import Link from "next/link";
import Decimal from "decimal.js";
import { formatDistanceToNow } from "date-fns";
import { ExternalLink } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { useSavingsActivity } from "@/hooks/useSavingsActivity";
import { getContractId } from "@/lib/contracts";
import config from "@/lib/config";
import type { SavingsActivityItem } from "@/lib/stellar/activity";
import { formatUsdc, MINUS } from "@/components/savings-rate/format";
import {
  BUTTON_PRIMARY,
  BUTTON_SECONDARY,
  EmptyPanel,
  PageHeader,
  Panel,
  SKELETON,
} from "@/components/ui/page-kit";

function sum(items: SavingsActivityItem[], kind: SavingsActivityItem["kind"]) {
  return items
    .filter((i) => i.kind === kind)
    .reduce((total, i) => total.plus(i.amount), new Decimal(0))
    .toFixed();
}

function ActivityRow({ item }: { item: SavingsActivityItem }) {
  const date = new Date(item.createdAt);
  const isDeposit = item.kind === "deposit";
  return (
    <li className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-6 gap-y-1 py-4 sm:grid-cols-[minmax(0,1fr)_auto_auto]">
      <div>
        <p className="text-sm text-foreground">{isDeposit ? "Deposit" : "Withdraw"}</p>
        <time
          dateTime={item.createdAt}
          title={date.toLocaleString()}
          className="text-[13px] text-muted-foreground"
        >
          {formatDistanceToNow(date, { addSuffix: true })}
        </time>
      </div>
      <p className="text-right text-sm tabular-nums text-foreground">
        {isDeposit ? "+" : MINUS}
        {formatUsdc(item.amount)}
      </p>
      <a
        href={`${config.explorerUrl}/tx/${item.hash}`}
        target="_blank"
        rel="noopener noreferrer"
        className="col-span-2 inline-flex items-center gap-1 text-[13px] text-muted-foreground transition-colors hover:text-foreground sm:col-span-1"
      >
        View on explorer
        <ExternalLink aria-hidden className="h-3 w-3" />
      </a>
    </li>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <Panel title={label}>
      <p className="mt-2 font-heading text-3xl font-light tabular-nums text-foreground">
        {formatUsdc(value)}
      </p>
    </Panel>
  );
}

function ActivityContent() {
  const { data, isLoading, error, refetch, isRefetching, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useSavingsActivity();

  if (isLoading) {
    return (
      <div data-testid="activity-loading" aria-busy="true" className="space-y-6">
        <div className="grid gap-6 sm:grid-cols-2">
          <div className={`h-28 ${SKELETON}`} />
          <div className={`h-28 ${SKELETON}`} />
        </div>
        <div className={`h-64 ${SKELETON}`} />
      </div>
    );
  }

  if (error || !data) {
    return (
      <EmptyPanel
        title="Your activity couldn't be loaded."
        action={
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isRefetching}
            className={BUTTON_PRIMARY}
          >
            {isRefetching ? "Retrying…" : "Try again"}
          </button>
        }
      >
        We read your history straight from the Stellar network, and it didn&apos;t respond. Check
        your connection and try again.
      </EmptyPanel>
    );
  }

  const items = data.pages.flatMap((p) => p.items);
  const scanned = data.pages.reduce((n, p) => n + p.scanned, 0);

  if (items.length === 0 && !hasNextPage) {
    return (
      <EmptyPanel
        title="No savings activity yet"
        action={
          <Link href="/dashboard" className={BUTTON_PRIMARY}>
            Start saving
          </Link>
        }
      >
        Deposits and withdrawals you make will show up here, each with a link to the transaction.
      </EmptyPanel>
    );
  }

  const suffix = hasNextPage ? ` in the last ${scanned} transactions` : "";

  return (
    <div className="space-y-6">
      <div className="grid gap-6 sm:grid-cols-2">
        <Stat label={`Deposited${suffix}`} value={sum(items, "deposit")} />
        <Stat label={`Withdrawn${suffix}`} value={sum(items, "withdraw")} />
      </div>

      <Panel title="History">
        {items.length === 0 ? (
          <p className="mt-4 text-sm text-muted-foreground">
            No deposits or withdrawals in the last {scanned} transactions. Load more to look further
            back.
          </p>
        ) : (
          <ul className="mt-2 divide-y">
            {items.map((item) => (
              <ActivityRow key={item.id} item={item} />
            ))}
          </ul>
        )}
        {hasNextPage && (
          <button
            type="button"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className={`mt-4 ${BUTTON_SECONDARY}`}
          >
            {isFetchingNextPage ? "Loading…" : "Load more"}
          </button>
        )}
      </Panel>
    </div>
  );
}

export default function ActivityPage() {
  const { isConnected, address } = useWallet();
  const live = !!getContractId("savingsCore");

  return (
    <AppShell>
      <PageHeader
        title="Activity"
        lede={
          <p className="text-sm text-muted-foreground">
            Your deposits into and withdrawals from the Nester Savings Rate, read from Stellar.
          </p>
        }
      />
      <div className="mt-8">
        {!isConnected || !address ? (
          <EmptyPanel
            title="Connect your wallet to see your activity."
            action={
              <Link href="/" className={BUTTON_PRIMARY}>
                Connect wallet
              </Link>
            }
          />
        ) : !live ? (
          <EmptyPanel title="The Savings Rate isn't live on this network yet.">
            Once the savings contract is deployed, your deposits and withdrawals will show up here.
          </EmptyPanel>
        ) : (
          <ActivityContent />
        )}
      </div>
    </AppShell>
  );
}
