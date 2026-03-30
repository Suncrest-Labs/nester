"use client";

import { useWallet } from "@/components/wallet-provider";
import { ConnectWallet } from "@/components/connect-wallet";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

export default function Home() {
    const { isConnected, isInitializing } = useWallet();
    const router = useRouter();

    useEffect(() => {
        if (!isInitializing && isConnected) {
            router.push("/dashboard");
        }
    }, [isConnected, isInitializing, router]);

    if (isInitializing || isConnected) return null;

    return <ConnectWallet />;
}
