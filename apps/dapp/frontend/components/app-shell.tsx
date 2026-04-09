"use client";

import Link from "next/link";
import Image from "next/image";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import {
    BarChart3,
    CandlestickChart,
    ChevronDown,
    Copy,
    ExternalLink,
    Globe,
    LayoutDashboard,
    LogOut,
    Menu,
    PiggyBank,
    User,
    Vault,
    X,
} from "lucide-react";
import { useWallet } from "@/components/wallet-provider";
import { useNetwork } from "@/hooks/useNetwork";
import { truncateAddress, cn } from "@/lib/utils";
import { AnimatePresence, motion } from "framer-motion";
import { NotificationBell } from "@/components/notification-bell";

// ── Nav items ────────────────────────────────────────────────────────────────

const SIDEBAR_NAV = [
    { label: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
    { label: "Savings", href: "/savings", icon: PiggyBank },
    { label: "Markets", href: "/vaults", icon: Vault },
    { label: "Stocks", href: "/stocks", icon: CandlestickChart },
    { label: "Offramp", href: "/offramp", icon: Globe },
    { label: "Portfolio", href: "/portfolio", icon: BarChart3 },
];

// ── Sidebar ──────────────────────────────────────────────────────────────────

function Sidebar({ bannerOffset }: { bannerOffset: boolean }) {
    const pathname = usePathname();

    return (
        <aside
            className={cn(
                "fixed left-0 bottom-0 z-40 hidden w-[240px] flex-col border-r border-black/[0.06] bg-white lg:flex",
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
                <span className="text-[15px] font-semibold text-black tracking-[-0.01em]">
                    Nester
                </span>
            </Link>

            {/* Nav links */}
            <nav className="flex-1 px-4 space-y-1">
                {SIDEBAR_NAV.map((item) => {
                    const active = pathname === item.href || pathname.startsWith(item.href + "/");
                    return (
                        <Link
                            key={item.href}
                            href={item.href}
                            className={cn(
                                "flex items-center gap-3 rounded-xl px-4 py-3 text-[14px] font-medium transition-colors",
                                active
                                    ? "bg-black/[0.04] text-black"
                                    : "text-black/40 hover:bg-black/[0.02] hover:text-black/60"
                            )}
                        >
                            <item.icon className="h-[18px] w-[18px] shrink-0" />
                            {item.label}
                        </Link>
                    );
                })}
            </nav>

            {/* Bottom links */}
            <div className="border-t border-black/[0.06] px-4 py-5 space-y-1">
                <Link
                    href="https://docs.nester.finance"
                    target="_blank"
                    className="flex items-center gap-3 rounded-xl px-4 py-3 text-[14px] font-medium text-black/35 hover:bg-black/[0.02] hover:text-black/55 transition-colors"
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
                    "sticky z-30 flex items-center justify-between gap-6 border-b border-black/[0.06] bg-white/80 backdrop-blur-md px-6 py-4 lg:px-10",
                    bannerOffset ? "top-10" : "top-0"
                )}
            >
                {/* Mobile: logo + hamburger */}
                <div className="flex items-center gap-3 lg:hidden">
                    <Link href="/" className="flex items-center gap-2">
                        <Image src="/logo.png" alt="Nester" width={28} height={28} className="rounded-lg" />
                        <span className="text-[14px] font-semibold text-black">Nester</span>
                    </Link>
                </div>

                {/* Spacer so right-side items push to the end */}
                <div className="hidden lg:block flex-1" />

                <div className="flex items-center gap-3">
                    {/* Notification bell */}
                    <NotificationBell />

                    {/* Wallet pill + dropdown */}
                    {address && (
                        <div className="relative" onClick={(e) => e.stopPropagation()}>
                            <button
                                onClick={() => setWalletMenuOpen((prev) => !prev)}
                                className="flex items-center gap-2 rounded-full border border-black/[0.08] px-4 py-2 shrink-0 transition-colors hover:border-black/20"
                            >
                                <div className="h-2 w-2 rounded-full bg-emerald-500" />
                                <span className="text-[13px] font-medium text-black/60 font-mono">
                                    {truncateAddress(address, 5)}
                                </span>
                                <ChevronDown className={cn("h-3 w-3 text-black/30 transition-transform", walletMenuOpen && "rotate-180")} />
                            </button>

                            <AnimatePresence>
                                {walletMenuOpen && (
                                    <motion.div
                                        initial={{ opacity: 0, y: 6, scale: 0.97 }}
                                        animate={{ opacity: 1, y: 0, scale: 1 }}
                                        exit={{ opacity: 0, y: 6, scale: 0.97 }}
                                        transition={{ duration: 0.12 }}
                                        className="absolute right-0 top-full mt-2 w-56 overflow-hidden rounded-xl border border-black/[0.08] bg-white shadow-lg z-50"
                                    >
                                        <div className="px-4 py-3 border-b border-black/[0.06]">
                                            <p className="text-[11px] text-black/35">Connected Wallet</p>
                                            <p className="mt-0.5 text-[12px] font-mono text-black/60 truncate">{address}</p>
                                        </div>
                                        <div className="py-1">
                                            <button
                                                onClick={copyAddress}
                                                className="flex w-full items-center gap-2.5 px-4 py-2.5 text-[13px] text-black/60 transition-colors hover:bg-black/[0.03] hover:text-black"
                                            >
                                                <Copy className="h-3.5 w-3.5" />
                                                {copied ? "Copied!" : "Copy Address"}
                                            </button>
                                            <Link
                                                href="/portfolio"
                                                className="flex w-full items-center gap-2.5 px-4 py-2.5 text-[13px] text-black/60 transition-colors hover:bg-black/[0.03] hover:text-black"
                                            >
                                                <User className="h-3.5 w-3.5" />
                                                Portfolio
                                            </Link>
                                            <button
                                                onClick={() => disconnect()}
                                                className="flex w-full items-center gap-2.5 px-4 py-2.5 text-[13px] text-red-500/70 transition-colors hover:bg-red-50 hover:text-red-600"
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
                        className="lg:hidden flex h-9 w-9 items-center justify-center rounded-xl border border-black/[0.08] text-black/50"
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
                            className="fixed inset-0 z-40 bg-black/20 backdrop-blur-sm lg:hidden"
                            onClick={() => setMobileOpen(false)}
                        />
                        <motion.div
                            initial={{ opacity: 0, y: -8 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -8 }}
                            className="fixed left-4 right-4 top-24 z-50 rounded-2xl border border-black/[0.06] bg-white shadow-xl lg:hidden"
                        >
                            <nav className="p-3 space-y-0.5">
                                {SIDEBAR_NAV.map((item) => {
                                    const active = pathname === item.href;
                                    return (
                                        <Link
                                            key={item.href}
                                            href={item.href}
                                            onClick={() => setMobileOpen(false)}
                                            className={cn(
                                                "flex items-center gap-3 rounded-xl px-4 py-3 text-[14px] font-medium transition-colors",
                                                active
                                                    ? "bg-black text-white"
                                                    : "text-black/60 hover:bg-black/5"
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
        <div className="min-h-screen bg-[#fafafa]">
            <Sidebar bannerOffset={bannerOffset} />
            <div className="lg:ml-[240px]">
                <TopBar bannerOffset={bannerOffset} />
                <main className="mx-auto max-w-[1120px] px-6 pt-10 pb-8 lg:px-10 lg:pt-12 lg:pb-10">
                    {children}
                </main>
            </div>
        </div>
    );
}
