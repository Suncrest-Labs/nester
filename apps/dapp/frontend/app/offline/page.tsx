"use client";

import Link from "next/link";
import Image from "next/image";
import { BUTTON_PRIMARY } from "@/components/ui/page-kit";

export default function OfflinePage() {
    return (
        <div className="flex min-h-screen flex-col items-center justify-center bg-background px-4">
            <Link href="/" className="mb-16 flex items-center gap-2.5">
                <Image src="/logo.png" alt="Nester" width={32} height={32} className="rounded-lg" />
                <span className="text-[15px] font-semibold text-foreground">Nester</span>
            </Link>

            <div className="w-full max-w-md text-center">
                <h1 className="text-[16px] font-semibold text-foreground">You&apos;re offline</h1>
                <p className="mt-2 text-sm text-muted-foreground">
                    This page needs a connection to load. Check your network and try again. Your
                    balances will sync again once you&apos;re back online.
                </p>

                <button
                    type="button"
                    onClick={() => window.location.reload()}
                    className={`${BUTTON_PRIMARY} mt-8`}
                >
                    Try again
                </button>
            </div>
        </div>
    );
}
