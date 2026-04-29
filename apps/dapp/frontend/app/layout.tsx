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
import { ReactQueryProvider } from "@/components/react-query-provider";
import { AuthProvider } from "@/components/auth-provider";
import { BottomNav } from "@/components/bottom-nav";
import { MotionConfig } from "framer-motion";

export default function RootLayout({
    children,
}: Readonly<{
    children: React.ReactNode;
}>) {
    return (
        <html lang="en" suppressHydrationWarning>
            <body
                suppressHydrationWarning
                className={`${inter.className} ${inter.variable} antialiased md:pb-0 mobile-content-pad`}
            >
                <MotionConfig reducedMotion="user">
                    <ReactQueryProvider>
                        <NetworkProvider>
                            <SettingsProvider>
                                <WalletProvider>
                                    <AuthProvider>
                                        <NotificationsProvider>
                                            <NetworkBanner />
                                            <PortfolioProvider>
                                                <WebSocketProvider>
                                                    <OnboardingProvider>
                                                        <a href="#main-content" className="skip-link">
                                                            Skip to main content
                                                        </a>
                                                        <main id="main-content">
                                                            {children}
                                                        </main>
                                                        <BottomNav />
                                                        <NotificationsToaster />
                                                        <PrometheusChatbot />
                                                    </OnboardingProvider>
                                                </WebSocketProvider>
                                            </PortfolioProvider>
                                        </NotificationsProvider>
                                    </AuthProvider>
                                </WalletProvider>
                            </SettingsProvider>
                        </NetworkProvider>
                    </ReactQueryProvider>
                </MotionConfig>
            </body>
        </html>
    );
}
