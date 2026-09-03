"use client";

import { useWallet } from "@/components/wallet-provider";
import { ConnectWallet } from "@/components/connect-wallet";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

export default function Home() {
    const { isConnected } = useWallet();
    const router = useRouter();

    // The connected wallet is the only gate. A separate "has onboarded" flag
    // in localStorage used to sit in front of this, which meant a returning
    // user with a live wallet was sent back to the welcome screen whenever
    // that flag was missing or written late.
    useEffect(() => {
        if (isConnected) {
            router.push("/dashboard");
        }
    }, [isConnected, router]);

    if (isConnected) return null;

    return <ConnectWallet />;
}
