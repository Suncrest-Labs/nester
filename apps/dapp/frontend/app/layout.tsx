import type { Metadata } from "next";
import { Space_Grotesk, Inter, Cormorant } from "next/font/google";
import { PortfolioProvider } from "@/components/portfolio-provider";
import { WalletProvider } from "@/components/wallet-provider";
import { NotificationsProvider } from "@/components/notifications-provider";
import { NotificationsToaster } from "@/components/notifications-toaster";
import { WebSocketProvider } from "@/components/websocket-provider";
import "./globals.css";

const spaceGrotesk = Space_Grotesk({
    subsets: ["latin"],
    variable: "--font-space-grotesk",
    display: "swap",
});

const inter = Inter({
    subsets: ["latin"],
    variable: "--font-inter",
    display: "swap",
});

const cormorant = Cormorant({
    subsets: ["latin"],
    weight: ["300", "400"],
    style: ["normal", "italic"],
    variable: "--font-cormorant",
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

export default function RootLayout({
    children,
}: Readonly<{
    children: React.ReactNode;
}>) {
    return (
        <html lang="en" suppressHydrationWarning>
            <body
                suppressHydrationWarning
                className={`${spaceGrotesk.variable} ${inter.variable} ${cormorant.variable} antialiased`}
            >
                <NetworkProvider>
                    <NetworkBanner />
                    <SettingsProvider>
                        <WalletProvider>
                            <NotificationsProvider>
                                <PortfolioProvider>
                                    <WebSocketProvider>
                                        <OnboardingProvider>
                                            {children}
                                            <NotificationsToaster />
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
