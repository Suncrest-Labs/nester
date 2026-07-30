"use client";

import Link from "next/link";
import Image from "next/image";
import { WifiOff, RefreshCw } from "lucide-react";

export default function OfflinePage() {
    return (
        <div className="min-h-screen bg-white dark:bg-[#100F0F] flex flex-col items-center justify-center px-4">
            <Link href="/" className="flex items-center gap-2.5 mb-16">
                <Image
                    src="/logo.png"
                    alt="Nester"
                    width={36}
                    height={36}
                    className="rounded-xl"
                />
                <span className="font-heading text-[15px] font-medium text-black dark:text-white">
                    Nester
                </span>
            </Link>

            <div className="flex h-16 w-16 items-center justify-center rounded-full bg-black/[0.04] dark:bg-white/[0.06] mb-6">
                <WifiOff className="h-7 w-7 text-black/40 dark:text-white/40" />
            </div>

            <div className="text-center">
                <h1 className="text-xl sm:text-2xl text-black dark:text-white mb-3">
                    You&apos;re offline
                </h1>
                <p className="text-sm text-black/40 dark:text-white/40 max-w-sm leading-relaxed">
                    This page needs a connection to load. Check your network
                    and try again. Your balances and vaults will resume
                    syncing automatically once you&apos;re back online.
                </p>
            </div>

            <button
                onClick={() => window.location.reload()}
                className="mt-10 inline-flex items-center gap-2 rounded-full bg-black dark:bg-blue-600 px-5 py-2.5 text-sm text-white transition-opacity hover:opacity-75"
            >
                <RefreshCw className="h-3.5 w-3.5" />
                Try again
            </button>

            <p className="mt-16 font-mono text-[11px] text-black/20 dark:text-white/20">
                nester.finance
            </p>
        </div>
    );
}
