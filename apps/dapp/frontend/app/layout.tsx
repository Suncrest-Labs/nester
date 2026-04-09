import type { Metadata } from "next";
import { Inter } from "next/font/google";
import { PortfolioProvider } from "@/components/portfolio-provider";
import { WalletProvider } from "@/components/wallet-provider";
import { NotificationsProvider } from "@/components/notifications-provider";
import { NotificationsToaster } from "@/components/notifications-toaster";
import { WebSocketProvider } from "@/components/websocket-provider";
import "./globals.css";

const inter = Inter({
    subsets: ["latin"],
    variable: "--font-inter",
    display: "swap",
});

export const metadata: Metadata = {
    title: "Nester | DApp",
    description:
        "Decentralized savings and instant fiat settlements powered by Stellar.",
    icons: {
        icon: "/logo.png",
        apple: "/logo.png",
    },
};

import { SettingsProvider } from "@/context/settings-context";
import { OnboardingProvider } from "@/hooks/useOnboarding";
import { NetworkProvider } from "@/context/NetworkProvider";
import { NetworkBanner } from "@/components/network/NetworkSelector";
import { PrometheusChatbot } from "@/components/ai/prometheusChatbot";

export default function RootLayout({
    children,
}: Readonly<{
    children: React.ReactNode;
}>) {
    return (
        <html lang="en" suppressHydrationWarning>
            <body
                suppressHydrationWarning
                className={`${inter.className} ${inter.variable} antialiased`}
            >
                <NetworkProvider>
                    <SettingsProvider>
                        <WalletProvider>
                            <NotificationsProvider>
                                <NetworkBanner />
                                <PortfolioProvider>
                                    <WebSocketProvider>
                                        <OnboardingProvider>
                                            {children}
                                            <NotificationsToaster />
                                            <PrometheusChatbot />
                                        </OnboardingProvider>
                                    </WebSocketProvider>
                                </PortfolioProvider>
                            </NotificationsProvider>
                        </WalletProvider>
                    </SettingsProvider>
                </NetworkProvider>
            </body>
        </html>
    );
}
