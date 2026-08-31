"use client";

import { useWallet } from "@/components/wallet-provider";
import { ConnectWallet } from "@/components/connect-wallet";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { WelcomeModal } from "@/components/onboarding/WelcomeModal";
import { TestnetSetupStepper } from "@/components/onboarding/TestnetSetupStepper";
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
            {/* First-run testnet setup (#1127). This is the surface a
                disconnected first-time visitor actually sees — /dashboard
                returns null without a wallet — so the install, network and
                funding steps have to live here. The deposit step is picked up
                on the dashboard once the wallet connects. */}
            <div className="mx-auto w-full max-w-2xl px-4 pt-6">
                <TestnetSetupStepper />
            </div>
            <ConnectWallet />
            <WelcomeModal />
        </>
    );
}
