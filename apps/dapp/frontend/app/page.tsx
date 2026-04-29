"use client";

import { useWallet } from "@/components/wallet-provider";
import { ConnectWallet } from "@/components/connect-wallet";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { WelcomeModal } from "@/components/onboarding/WelcomeModal";
import { useOnboarding } from "@/hooks/useOnboarding";

export default function Home() {
    const { isConnected } = useWallet();
    const { hasConnectedWallet } = useOnboarding();
    const router = useRouter();

    useEffect(() => {
        if (isConnected && hasConnectedWallet) {
            router.push("/dashboard");
        }
    }, [isConnected, hasConnectedWallet, router]);

    if (isConnected && hasConnectedWallet) return null;

    return (
        <>
            <ConnectWallet />
            <WelcomeModal />
        </>
    );
}
