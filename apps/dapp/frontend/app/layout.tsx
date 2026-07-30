import type { Metadata, Viewport } from "next";
import Script from "next/script";
import { Inter } from "next/font/google";
import { PortfolioProvider } from "@/components/portfolio-provider";
import { WalletProvider } from "@/components/wallet-provider";
import { AuthProvider } from "@/components/auth-provider";
import { NotificationsProvider } from "@/components/notifications-provider";
import { NotificationsToaster } from "@/components/notifications-toaster";
import { WebSocketProvider } from "@/components/websocket-provider";
import { ReactQueryProvider } from "@/components/react-query-provider";
import { OfflineBanner } from "@/components/offline-banner";
import { SettingsProvider } from "@/context/settings-context";
import { LocaleProvider } from "@/context/locale-context";
import { OnboardingProvider } from "@/hooks/useOnboarding";
import { NetworkProvider } from "@/context/NetworkProvider";
import { NetworkBanner } from "@/components/network/NetworkSelector";
import { ServiceWorkerRegister } from "@/components/service-worker-register";
import "./globals.css";

const inter = Inter({ subsets: ["latin"], variable: "--font-inter" });

export const metadata: Metadata = {
    title: "Nester | DApp",
    description:
        "Decentralized savings and instant fiat settlements powered by Stellar.",
    manifest: "/manifest.webmanifest",
    icons: {
        icon: "/logo.png",
        apple: "/logo.png",
    },
};

export const viewport: Viewport = {
    themeColor: "#0a0a0a",
};

import { ToastProvider } from "@/components/ui/toast/toast-provider";

import { ConsentProvider } from "@/context/consent-context";
import { ConsentGatedPrometheus } from "@/components/consent-gated-prometheus";
import { CookieConsentBanner } from "@/components/cookie-consent-banner";

const themeInitScript = `
(function() {
  try {
    var key = 'nester-theme';
    var legacy = 'nester_theme';
    var stored = localStorage.getItem(key) || localStorage.getItem(legacy) || 'light';
    if (stored === 'system') {
      stored = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    if (stored === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  } catch (e) {}
})();
`;

export default function RootLayout({
    children,
}: Readonly<{
    children: React.ReactNode;
}>) {
    return (
        <html lang="en" suppressHydrationWarning>
            <head>
                <Script
                    id="theme-init"
                    strategy="beforeInteractive"
                    dangerouslySetInnerHTML={{ __html: themeInitScript }}
                />
            </head>
            <body
                suppressHydrationWarning
                className={`${inter.className} ${inter.variable} antialiased`}
            >
                <ToastProvider>
                    <ConsentProvider>
                        <ReactQueryProvider>
                            <NetworkProvider>
                                <LocaleProvider>
                                    <SettingsProvider>
                                        <WalletProvider>
                                            <AuthProvider>
                                                <NotificationsProvider>
                                                    <ServiceWorkerRegister />
                                                    <OfflineBanner />
                                                    <NetworkBanner />
                                                    <PortfolioProvider>
                                                        <WebSocketProvider>
                                                            <OnboardingProvider>
                                                                {children}
                                                                <NotificationsToaster />
                                                                <ConsentGatedPrometheus />
                                                                <CookieConsentBanner />
                                                            </OnboardingProvider>
                                                        </WebSocketProvider>
                                                    </PortfolioProvider>
                                                </NotificationsProvider>
                                            </AuthProvider>
                                        </WalletProvider>
                                    </SettingsProvider>
                                </LocaleProvider>
                            </NetworkProvider>
                        </ReactQueryProvider>
                    </ConsentProvider>
                </ToastProvider>
            </body>
        </html>
    );
}
