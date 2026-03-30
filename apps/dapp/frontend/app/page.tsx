"use client";

import { useWallet } from "@/components/wallet-provider";
import { ConnectWallet } from "@/components/connect-wallet";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { WelcomeModal } from "@/components/onboarding/WelcomeModal";
import { useOnboarding } from "@/hooks/useOnboarding";

export default function Home() {
    const { isConnected, isInitializing } = useWallet();
    const { hasConnectedWallet } = useOnboarding();
    const router = useRouter();

    useEffect(() => {
        if (!isInitializing && isConnected && hasConnectedWallet) {
            router.push("/dashboard");
        }
    }, [isConnected, isInitializing, hasConnectedWallet, router]);

    if (isInitializing || (isConnected && hasConnectedWallet)) return null;

    return (
        <>
            <ConnectWallet />
            <WelcomeModal />
        </>
    );
}
