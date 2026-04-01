"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useWallet } from "@/components/wallet-provider";
import { NotificationBell } from "@/components/notification-bell";
import { truncateAddress, cn } from "@/lib/utils";
import { LogOut, Copy, Check, ChevronDown } from "lucide-react";
import { useEffect, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";


export function Navbar() {
    const pathname = usePathname();
    const { address, isConnected, disconnect } = useWallet();
    const [copied, setCopied] = useState(false);
    const [showMenu, setShowMenu] = useState(false);
    const [isScrolled, setIsScrolled] = useState(false);

    useEffect(() => {
        const handleScroll = () => {
            setIsScrolled(window.scrollY > 20);
        };
        window.addEventListener("scroll", handleScroll, { passive: true });
        return () => window.removeEventListener("scroll", handleScroll);
    }, []);

    // Close menu on outside click
    useEffect(() => {
        if (!showMenu) return;
        const handleClick = () => setShowMenu(false);
        document.addEventListener("click", handleClick);
        return () => document.removeEventListener("click", handleClick);
    }, [showMenu]);

    const copyAddress = (e: React.MouseEvent) => {
        e.stopPropagation();
        if (address) {
            navigator.clipboard.writeText(address);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        }
    };

    return (
        <nav
            className={cn(
                "fixed top-10 left-0 right-0 z-50 transition-all duration-300 border-b",
                isScrolled
                    ? "bg-white/80 backdrop-blur-md border-border shadow-sm py-3"
                    : "bg-white/60 backdrop-blur-sm border-transparent py-4"
            )}
        >
            <div className="mx-auto max-w-384 px-4 md:px-8 lg:px-12 xl:px-16">
                <div className="flex items-center justify-between">
                    <Link href="/" className="flex items-center gap-2.5 group">
                        <Image
                            src="/logo.png"
                            alt="Nester"
                            width={36}
                            height={36}
                            className="rounded-xl"
                        />
                        <span className="font-heading text-[15px] font-medium text-foreground">
                            Nester
                        </span>
                    </Link>

                    {isConnected && (
                        <div className="hidden md:flex items-center gap-8">
                            {[
                                { label: "Dashboard", href: "/dashboard" },
                                { label: "Vaults", href: "/dashboard/vaults" },
                                {
                                    label: "Offramp",
                                    href: "/dashboard/settlements",
                                },
                                { label: "Portfolio", href: "/dashboard/portfolio" },
                            ].map((item) => (
                                <Link
                                    key={item.label}
                                    href={item.href}
                                    data-tour={item.label === "Offramp" ? "settlements-tab" : undefined}
                                    className={cn(
                                        "text-[15px] font-medium transition-colors relative py-2",
                                        pathname === item.href
                                            ? "text-foreground"
                                            : "text-foreground/50 hover:text-foreground/80"
                                    )}
                                >
                                    {item.label}
                                    {pathname === item.href && (
                                        <span className="absolute bottom-0 left-0 right-0 h-0.5 rounded-full bg-foreground/80" />
                                    )}
                                </Link>
                            ))}
                        </div>
                    )}

                    <div className="flex items-center gap-3">
                        {isConnected && address ? (
                            <>
                                <NotificationBell />

                                <div
                                    className="relative"
                                    onClick={(e) => e.stopPropagation()}
                                >
                                    <button
                                        onClick={() => setShowMenu(!showMenu)}
                                        className="flex items-center gap-2 rounded-full border border-border bg-white px-4 py-2 transition-all hover:border-black/20 hover:shadow-sm"
                                    >
                                        <div className="h-2 w-2 rounded-full bg-emerald-500" />
                                        <span className="text-sm font-medium text-foreground font-mono">
                                            {truncateAddress(address, 5)}
                                        </span>
                                        <ChevronDown
                                            className={cn(
                                                "h-3.5 w-3.5 text-muted-foreground transition-transform",
                                                showMenu && "rotate-180"
                                            )}
                                        />
                                    </button>

                                    <AnimatePresence>
                                        {showMenu && (
                                            <motion.div
                                                initial={{
                                                    opacity: 0,
                                                    y: 8,
                                                    scale: 0.96,
                                                }}
                                                animate={{
                                                    opacity: 1,
                                                    y: 0,
                                                    scale: 1,
                                                }}
                                                exit={{
                                                    opacity: 0,
                                                    y: 8,
                                                    scale: 0.96,
                                                }}
                                                transition={{ duration: 0.15 }}
                                                className="absolute right-0 top-full mt-2 w-64 rounded-2xl border border-border bg-white p-2 shadow-xl shadow-black/8"
                                            >
                                                <div className="px-3 py-2 mb-1">
                                                    <p className="text-xs text-muted-foreground mb-1">
                                                        Connected Wallet
                                                    </p>
                                                    <p className="text-sm font-mono text-foreground/70 break-all">
                                                        {truncateAddress(
                                                            address,
                                                            10
                                                        )}
                                                    </p>
                                                </div>
                                                <button
                                                    onClick={copyAddress}
                                                    className="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-foreground/70 hover:bg-secondary hover:text-foreground transition-colors"
                                                >
                                                    {copied ? (
                                                        <Check className="h-4 w-4 text-emerald-500" />
                                                    ) : (
                                                        <Copy className="h-4 w-4" />
                                                    )}
                                                    {copied
                                                        ? "Copied!"
                                                        : "Copy Address"}
                                                </button>
                                                <Link
                                                    href="/dashboard/portfolio"
                                                    className="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-foreground/70 hover:bg-secondary hover:text-foreground transition-colors"
                                                    onClick={() => setShowMenu(false)}
                                                >
                                                    <span className="flex items-center gap-3">
                                                        <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/><polyline points="7 10 10 13 13 10 17 14"/></svg>
                                                        Portfolio
                                                    </span>
                                                </Link>
                                                <button
                                                    onClick={() => {
                                                        disconnect();
                                                        setShowMenu(false);
                                                    }}
                                                    className="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-destructive hover:bg-destructive/10 transition-colors"
                                                >
                                                    <LogOut className="h-4 w-4" />
                                                    Disconnect
                                                </button>
                                            </motion.div>
                                        )}
                                    </AnimatePresence>
                                </div>
                            </>
                        ) : (
                            <Link href="/">
                                <div className="p-0.5 rounded-full border border-black/15">
                                    <button className="rounded-full bg-brand-dark hover:bg-brand-dark/90 px-5 py-2 text-sm font-medium text-white transition-all">
                                        Connect Wallet
                                    </button>
                                </div>
                            </Link>
                        )}
                    </div>
                </div>
            </div>
        </nav>
    );
}
