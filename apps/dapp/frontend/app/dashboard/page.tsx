"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { LogIn, RefreshCw } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { useAuth } from "@/components/auth-provider";
import { useSavings } from "@/hooks/useSavings";
import { ApiError } from "@/lib/api/client";
import type { SavingsSummary } from "@/lib/api/savings";
import { RateHero } from "@/components/savings-rate/RateHero";
import { SavePanel } from "@/components/savings-rate/SavePanel";
import { PositionCard } from "@/components/savings-rate/PositionCard";
import { AllocationCard } from "@/components/savings-rate/AllocationCard";
import { ReserveCard } from "@/components/savings-rate/ReserveCard";
import { PendingChangesCard } from "@/components/savings-rate/PendingChangesCard";
import { SharePriceChart } from "@/components/savings-rate/SharePriceChart";

const PANEL = "rounded-xl border bg-card p-6";
const COLUMNS = "grid items-start gap-6 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]";

function plural(n: number, unit: string) {
    return `${n} ${unit}${n === 1 ? "" : "s"}`;
}

function formatDelay(seconds: number) {
    if (seconds >= 86_400 && seconds % 86_400 === 0) return plural(seconds / 86_400, "day");
    if (seconds >= 3_600 && seconds % 3_600 === 0) return plural(seconds / 3_600, "hour");
    return plural(Math.round(seconds / 60), "minute");
}

function SignInCard({ onSignIn, isLoading }: { onSignIn: () => Promise<unknown>; isLoading: boolean }) {
    const [failed, setFailed] = useState(false);

    function handleSignIn() {
        setFailed(false);
        onSignIn().catch(() => setFailed(true));
    }

    return (
        <div className={PANEL}>
            <h3 className="text-sm font-medium text-muted-foreground">Your balance</h3>
            <p className="mt-2 text-sm text-foreground/80">
                Sign in with your wallet to see what you hold.
            </p>
            {failed && (
                <p role="alert" className="mt-2 text-sm text-red-600 dark:text-red-400">
                    Sign-in didn&apos;t complete. Approve the request in your wallet and try again.
                </p>
            )}
            <button
                type="button"
                onClick={handleSignIn}
                disabled={isLoading}
                className="mt-4 inline-flex items-center gap-1.5 rounded-full bg-foreground px-4 py-2 text-[13px] font-medium text-background transition-opacity hover:opacity-80 disabled:opacity-50"
            >
                {isLoading ? <RefreshCw className="h-3 w-3 animate-spin" /> : <LogIn className="h-3 w-3" />}
                {isLoading ? "Signing in…" : "Sign in"}
            </button>
        </div>
    );
}

function GoalsCard() {
    return (
        <div className={PANEL}>
            <h3 className="text-sm font-medium text-muted-foreground">Goals</h3>
            <p className="mt-2 text-sm text-foreground/80">
                Pick a target amount and date, then track your progress toward it.
            </p>
            <Link
                href="/savings"
                className="mt-4 inline-flex rounded-full border px-4 py-2 text-[13px] font-medium text-foreground transition-colors hover:bg-foreground/5"
            >
                Set a goal
            </Link>
        </div>
    );
}

function MarketYieldsCard() {
    return (
        <div className={PANEL}>
            <h3 className="text-sm font-medium text-muted-foreground">Market yields</h3>
            <p className="mt-2 text-sm text-foreground/80">
                See what DeFi protocols on Stellar are paying right now.
            </p>
            <Link
                href="/yields"
                className="mt-4 inline-flex rounded-full border px-4 py-2 text-[13px] font-medium text-foreground transition-colors hover:bg-foreground/5"
            >
                Browse yields
            </Link>
        </div>
    );
}

function NotLive() {
    return (
        <div className={`mt-4 ${PANEL}`}>
            <p className="text-sm text-foreground/80">
                The Savings Rate isn&apos;t live on this network yet.
            </p>
            <p className="mt-2 max-w-[60ch] text-sm text-muted-foreground">
                This is a testnet build waiting for the savings contract to be deployed. In the
                meantime you can explore market yields or set a savings goal.
            </p>
            <div className="mt-4 flex flex-wrap gap-3">
                <Link
                    href="/yields"
                    className="inline-flex rounded-full bg-foreground px-4 py-2 text-[13px] font-medium text-background transition-opacity hover:opacity-80"
                >
                    Browse yields
                </Link>
                <Link
                    href="/savings"
                    className="inline-flex rounded-full border px-4 py-2 text-[13px] font-medium text-foreground transition-colors hover:bg-foreground/5"
                >
                    Set a goal
                </Link>
            </div>
        </div>
    );
}

function Rules({ summary }: { summary: SavingsSummary }) {
    const delay = formatDelay(summary.timelockSeconds);
    return (
        <div className="text-sm">
            <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]">
                <div>
                    <dt className="text-muted-foreground">Performance fee</dt>
                    <dd className="mt-0.5 text-foreground">
                        {summary.feeBps / 100}% of gains above the previous high
                    </dd>
                </div>
                <div>
                    <dt className="text-muted-foreground">Change delay</dt>
                    <dd className="mt-0.5 text-foreground">{delay}</dd>
                </div>
            </dl>
            {summary.pending.length === 0 && (
                <p className="mt-3 text-muted-foreground">
                    No changes are queued. New venues, higher caps and higher fees wait {delay} before
                    they apply.
                </p>
            )}
        </div>
    );
}

function LoadingState() {
    return (
        <div data-testid="savings-loading" aria-busy="true" className="mt-4 space-y-8">
            <div className="h-20 w-56 animate-pulse rounded-lg bg-foreground/[0.06]" />
            <div className={COLUMNS}>
                <div className="h-72 animate-pulse rounded-xl bg-foreground/[0.06]" />
                <div className="h-40 animate-pulse rounded-xl bg-foreground/[0.06]" />
            </div>
        </div>
    );
}

export default function Dashboard() {
    const { isConnected, address } = useWallet();
    const { isAuthenticated, isSigningIn, signIn } = useAuth();
    const router = useRouter();
    const { summary, position, positionLoading, positionError, isLoading, error } = useSavings();

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    // Auto sign-in fires at most once per connected address so a rejected
    // signature can't loop the wallet prompt; the sign-in card is the retry path.
    const autoSignInAttempted = useRef<string | null>(null);
    useEffect(() => {
        if (!isConnected || !address) {
            autoSignInAttempted.current = null;
            return;
        }
        if (isAuthenticated || isSigningIn) return;
        if (autoSignInAttempted.current === address) return;
        autoSignInAttempted.current = address;
        signIn().catch(() => {});
    }, [isConnected, address, isAuthenticated, isSigningIn, signIn]);

    if (!isConnected) {
        return (
            <main data-testid="dashboard-connect-prompt" className="mx-auto max-w-md px-5 py-24 text-center">
                <h1 className="text-lg font-semibold text-foreground">Connect your wallet</h1>
                <p className="mt-2 text-sm text-muted-foreground">
                    Taking you to the connect screen to open the Nester Savings Rate.
                </p>
                <Link
                    href="/"
                    className="mt-5 inline-flex rounded-full bg-foreground px-4 py-2 text-[13px] font-medium text-background transition-opacity hover:opacity-80"
                >
                    Connect wallet
                </Link>
            </main>
        );
    }

    return (
        <AppShell>
            <h1 className="text-[13px] font-medium text-muted-foreground">Nester Savings Rate</h1>

            {!summary ? (
                isLoading ? (
                    <LoadingState />
                ) : error instanceof ApiError && error.status === 503 ? (
                    <NotLive />
                ) : (
                    <p role="alert" className={`mt-4 ${PANEL} text-sm text-foreground/80`}>
                        Savings data couldn&apos;t be loaded. Check your connection and reload the page.
                    </p>
                )
            ) : (
                <>
                    <RateHero rate7d={summary.rate7d} rate30d={summary.rate30d} />

                    <div className={`mt-8 ${COLUMNS}`}>
                        <SavePanel sharePrice={summary.sharePrice} position={position} />
                        <div className="grid gap-6">
                            {position ? (
                                <PositionCard position={position} />
                            ) : !isAuthenticated ? (
                                <SignInCard onSignIn={signIn} isLoading={isSigningIn} />
                            ) : positionError ? (
                                <p className={`${PANEL} text-sm text-foreground/80`}>
                                    Couldn&apos;t load your balance. Reload the page to try again.
                                </p>
                            ) : positionLoading ? (
                                <div
                                    data-testid="position-loading"
                                    aria-busy="true"
                                    className="h-36 animate-pulse rounded-xl bg-foreground/[0.06]"
                                />
                            ) : null}
                            <GoalsCard />
                            <MarketYieldsCard />
                        </div>
                    </div>

                    <div className="mt-6">
                        <SharePriceChart />
                    </div>

                    <section aria-labelledby="transparency-heading" className="mt-14">
                        <h2 id="transparency-heading" className="text-[16px] font-semibold text-foreground">
                            Where your money is
                        </h2>
                        <p className="mt-1 max-w-[60ch] text-sm text-muted-foreground">
                            Read from the savings contract on Stellar. The rate follows what these
                            venues pay, so it can go down as well as up.
                        </p>
                        <div className="mt-5">
                            <Rules summary={summary} />
                        </div>
                        <div className={`mt-6 ${COLUMNS}`}>
                            <AllocationCard summary={summary} />
                            <div className="grid gap-6">
                                <ReserveCard reserve={summary.reserve} totalAssets={summary.totalAssets} />
                                <PendingChangesCard pending={summary.pending} />
                            </div>
                        </div>
                    </section>
                </>
            )}
        </AppShell>
    );
}
