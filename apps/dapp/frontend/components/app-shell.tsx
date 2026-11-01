"use client";

import Link from "next/link";
import Image from "next/image";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import {
    ChevronDown,
    Copy,
    ExternalLink,
    LogOut,
    Menu,
    Settings,
    X,
} from "lucide-react";
import { useWallet } from "@/components/wallet-provider";
import { useNetwork } from "@/hooks/useNetwork";
import { truncateAddress, cn } from "@/lib/utils";
import { APP_NAV as SIDEBAR_NAV } from "@/lib/navigation";
import { AnimatePresence, motion } from "framer-motion";
import { SiteFooter } from "@/components/site-footer";
import { NotificationBell } from "@/components/notification-bell";
import { ThemeToggle } from "@/components/theme-toggle";

const MENU_ITEM =
    "flex w-full items-center gap-2.5 px-4 py-2.5 text-[13px] text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground";
const QUIET_LINK =
    "flex items-center gap-3 rounded-xl px-4 py-3 text-[14px] font-medium text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground";

function isActive(pathname: string, href: string) {
    return pathname === href || pathname.startsWith(href + "/");
}

// ── Sidebar ──────────────────────────────────────────────────────────────────

function Sidebar({ bannerOffset }: { bannerOffset: boolean }) {
    const pathname = usePathname();

    return (
        <aside
            className={cn(
                "fixed left-0 bottom-0 z-40 hidden w-[240px] flex-col border-r bg-background lg:flex",
                bannerOffset ? "top-10" : "top-0"
            )}
        >
            {/* Logo */}
            <Link href="/" className="flex items-center gap-3 px-7 pt-7 pb-8">
                <Image
                    src="/logo.png"
                    alt="Nester"
                    width={30}
                    height={30}
                    className="rounded-lg"
                />
                <span className="text-[15px] font-semibold text-foreground">
                    Nester
                </span>
            </Link>

            {/* Nav links */}
            <nav className="flex-1 px-4 space-y-1">
                {SIDEBAR_NAV.map((item) => {
                    const active = isActive(pathname, item.href);
                    return (
                        <Link
                            key={item.href}
                            href={item.href}
                            aria-current={active ? "page" : undefined}
                            className={cn(
                                "flex items-center gap-3 rounded-xl px-4 py-3 text-[14px] font-medium transition-colors",
                                active
                                    ? "bg-foreground/5 text-foreground"
                                    : "text-muted-foreground hover:bg-foreground/5 hover:text-foreground"
                            )}
                        >
                            <item.icon className="h-[18px] w-[18px] shrink-0" />
                            {item.label}
                        </Link>
                    );
                })}
            </nav>

            {/* Bottom links */}
            <div className="border-t px-4 py-5 space-y-1">
                <Link
                    href="/settings"
                    aria-current={isActive(pathname, "/settings") ? "page" : undefined}
                    className={cn(
                        QUIET_LINK,
                        isActive(pathname, "/settings") && "bg-foreground/5 text-foreground"
                    )}
                >
                    <Settings className="h-[18px] w-[18px] shrink-0" />
                    Settings
                </Link>
                <Link
                    href="https://docs.nester.finance"
                    target="_blank"
                    rel="noopener noreferrer"
                    className={QUIET_LINK}
                >
                    <ExternalLink className="h-[18px] w-[18px] shrink-0" />
                    Developer Docs
                </Link>
            </div>
        </aside>
    );
}

// ── TopBar ────────────────────────────────────────────────────────────────────

function TopBar({ bannerOffset }: { bannerOffset: boolean }) {
    const { address, disconnect } = useWallet();
    const [mobileOpen, setMobileOpen] = useState(false);
    const [walletMenuOpen, setWalletMenuOpen] = useState(false);
    const [copied, setCopied] = useState(false);
    const pathname = usePathname();

    useEffect(() => {
        if (!walletMenuOpen) return;
        const handleClick = () => setWalletMenuOpen(false);
        document.addEventListener("click", handleClick);
        return () => document.removeEventListener("click", handleClick);
    }, [walletMenuOpen]);

    const copyAddress = () => {
        if (!address) return;
        navigator.clipboard.writeText(address);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
    };

    return (
        <>
            <header
                className={cn(
                    "sticky z-30 flex items-center justify-between gap-3 sm:gap-6 border-b bg-background/90 backdrop-blur-md px-4 py-4 sm:px-6 lg:px-10",
                    bannerOffset ? "top-10" : "top-0"
                )}
            >
                {/* Mobile: logo + hamburger */}
                <div className="flex items-center gap-3 lg:hidden">
                    <Link href="/" className="flex items-center gap-2">
                        <Image src="/logo.png" alt="Nester" width={28} height={28} className="rounded-lg" />
                        <span className="hidden text-[14px] font-semibold sm:inline text-foreground">Nester</span>
                    </Link>
                </div>

                {/* Spacer so right-side items push to the end */}
                <div className="hidden lg:block flex-1" />

                <div className="flex items-center gap-2 sm:gap-3">
                    <ThemeToggle />
                    {/* Notification bell */}
                    <NotificationBell />

                    {/* Wallet pill + dropdown */}
                    {address && (
                        <div className="relative" onClick={(e) => e.stopPropagation()}>
                            <button
                                onClick={() => setWalletMenuOpen((prev) => !prev)}
                                className="flex items-center gap-2 rounded-full border px-3 py-2 shrink-0 sm:px-4 transition-colors hover:bg-foreground/5"
                            >
                                <div className="h-2 w-2 rounded-full bg-emerald-500" />
                                <span className="text-[13px] font-medium text-foreground">
                                    {truncateAddress(address, 5)}
                                </span>
                                <ChevronDown className={cn("h-3 w-3 text-muted-foreground transition-transform", walletMenuOpen && "rotate-180")} />
                            </button>

                            <AnimatePresence>
                                {walletMenuOpen && (
                                    <motion.div
                                        initial={{ opacity: 0, y: 6, scale: 0.97 }}
                                        animate={{ opacity: 1, y: 0, scale: 1 }}
                                        exit={{ opacity: 0, y: 6, scale: 0.97 }}
                                        transition={{ duration: 0.12 }}
                                        className="absolute right-0 top-full mt-2 w-60 overflow-hidden rounded-xl border bg-card z-50"
                                    >
                                        <div className="px-4 py-3 border-b">
                                            <p className="text-[12px] text-muted-foreground">Connected wallet</p>
                                            <p className="mt-0.5 text-[12px] text-foreground truncate">{address}</p>
                                        </div>
                                        <div className="py-1">
                                            <button
                                                onClick={copyAddress}
                                                className={MENU_ITEM}
                                            >
                                                <Copy className="h-3.5 w-3.5" />
                                                {copied ? "Copied" : "Copy address"}
                                            </button>
                                            <Link
                                                href="/settings"
                                                className={MENU_ITEM}
                                            >
                                                <Settings className="h-3.5 w-3.5" />
                                                Settings
                                            </Link>
                                            <button
                                                onClick={() => disconnect()}
                                                className="flex w-full items-center gap-2.5 px-4 py-2.5 text-[13px] text-red-600 transition-colors hover:bg-foreground/5 dark:text-red-400"
                                            >
                                                <LogOut className="h-3.5 w-3.5" />
                                                Disconnect
                                            </button>
                                        </div>
                                    </motion.div>
                                )}
                            </AnimatePresence>
                        </div>
                    )}
                    {/* Mobile hamburger */}
                    <button
                        onClick={() => setMobileOpen(!mobileOpen)}
                        aria-label="Toggle navigation menu"
                        aria-expanded={mobileOpen}
                        className="lg:hidden flex h-9 w-9 items-center justify-center rounded-full border text-muted-foreground transition-colors hover:text-foreground"
                    >
                        {mobileOpen ? <X className="h-4 w-4" /> : <Menu className="h-4 w-4" />}
                    </button>
                </div>
            </header>

            {/* Mobile nav drawer */}
            <AnimatePresence>
                {mobileOpen && (
                    <>
                        <motion.div
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            className="fixed inset-0 z-40 bg-background/60 backdrop-blur-sm lg:hidden"
                            onClick={() => setMobileOpen(false)}
                        />
                        <motion.div
                            initial={{ opacity: 0, y: -8 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -8 }}
                            className="fixed left-4 right-4 top-24 z-50 rounded-xl border bg-card lg:hidden"
                        >
                            <nav className="p-3 space-y-0.5">
                                {SIDEBAR_NAV.map((item) => {
                                    const active = isActive(pathname, item.href);
                                    return (
                                        <Link
                                            key={item.href}
                                            href={item.href}
                                            aria-current={active ? "page" : undefined}
                                            onClick={() => setMobileOpen(false)}
                                            className={cn(
                                                "flex items-center gap-3 rounded-xl px-4 py-3 text-[14px] font-medium transition-colors",
                                                active
                                                    ? "bg-foreground text-background"
                                                    : "text-muted-foreground hover:bg-foreground/5 hover:text-foreground"
                                            )}
                                        >
                                            <item.icon className="h-[18px] w-[18px]" />
                                            {item.label}
                                        </Link>
                                    );
                                })}
                            </nav>
                        </motion.div>
                    </>
                )}
            </AnimatePresence>
        </>
    );
}

// ── AppShell ─────────────────────────────────────────────────────────────────

export function AppShell({ children }: { children: React.ReactNode }) {
    const { currentNetwork } = useNetwork();
    const bannerOffset = currentNetwork.id === "testnet";

    return (
        <div className="min-h-screen bg-background">
            <Sidebar bannerOffset={bannerOffset} />
            <div className="flex min-h-screen flex-col lg:ml-[240px]">
                <TopBar bannerOffset={bannerOffset} />
                <main className="mx-auto w-full max-w-[1120px] flex-1 px-5 pt-10 sm:px-8 pb-8 lg:px-16 lg:pt-14 lg:pb-12">
                    {children}
                </main>
                <SiteFooter />
            </div>
        </div>
    );
}
