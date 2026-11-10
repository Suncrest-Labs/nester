"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useWallet } from "@/components/wallet-provider";
import { NotificationBell } from "@/components/notification-bell";
import { ConnectionStatusBadge } from "@/components/connection-status-badge";
import { truncateAddress, cn } from "@/lib/utils";
import { LogOut, Copy, Check, ChevronDown, Menu, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useNetwork } from "@/hooks/useNetwork";
import { motion, AnimatePresence } from "framer-motion";
import { APP_NAV as NAV_LINKS } from "@/lib/navigation";

export function Navbar() {
    const pathname = usePathname();
    const { address, isConnected, disconnect } = useWallet();
    const { currentNetwork } = useNetwork();
    const [copied, setCopied] = useState(false);
    const [showMenu, setShowMenu] = useState(false);
    const [mobileOpen, setMobileOpen] = useState(false);
    const [isScrolled, setIsScrolled] = useState(false);

    useEffect(() => {
        const handleScroll = () => setIsScrolled(window.scrollY > 20);
        window.addEventListener("scroll", handleScroll, { passive: true });
        return () => window.removeEventListener("scroll", handleScroll);
    }, []);

    // Close wallet menu on outside click
    useEffect(() => {
        if (!showMenu) return;
        const handleClick = () => setShowMenu(false);
        document.addEventListener("click", handleClick);
        return () => document.removeEventListener("click", handleClick);
    }, [showMenu]);

    // Lock body scroll when mobile menu is open
    useEffect(() => {
        if (mobileOpen) {
            document.documentElement.classList.add("menu-open");
        } else {
            document.documentElement.classList.remove("menu-open");
        }
        return () => document.documentElement.classList.remove("menu-open");
    }, [mobileOpen]);

    // Use prev path state to detect changes instead of effect
    const [prevPath, setPrevPath] = useState(pathname);
    if (pathname !== prevPath) {
        setPrevPath(pathname);
        if (mobileOpen) setMobileOpen(false);
    }

    const isActive = (href: string) => pathname === href || pathname.startsWith(href + "/");

    const copyAddress = (e: React.MouseEvent) => {
        e.stopPropagation();
        if (address) {
            navigator.clipboard.writeText(address);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        }
    };

    return (
        <>
            <nav
                className={cn(
                    "fixed left-0 right-0 z-50 transition-all duration-300 border-b",
                    currentNetwork.id === 'testnet' ? "top-10" : "top-0",
                    isScrolled
                        ? "bg-background/90 backdrop-blur-md border-border py-3"
                        : "bg-background/70 backdrop-blur-sm border-transparent py-4"
                )}
            >
                <div className="mx-auto max-w-7xl px-4 md:px-8 lg:px-12 xl:px-16">
                    <div className="flex items-center justify-between">
                        {/* Logo */}
                        <Link href="/" className="flex items-center gap-2.5">
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

                        {/* Desktop nav */}
                        {isConnected && (
                            <div className="hidden md:flex items-center gap-8">
                                {NAV_LINKS.map((item) => (
                                    <Link
                                        key={item.label}
                                        href={item.href}
                                        data-tour={undefined}
                                        className={cn(
                                            "text-[15px] font-medium transition-colors relative py-2",
                                            isActive(item.href)
                                                ? "text-foreground"
                                                : "text-muted-foreground hover:text-foreground"
                                        )}
                                    >
                                        {item.label}
                                        {isActive(item.href) && (
                                            <span className="absolute bottom-0 left-0 right-0 h-0.5 rounded-full bg-foreground" />
                                        )}
                                    </Link>
                                ))}
                            </div>
                        )}

                        {/* Right side */}
                        <div className="flex items-center gap-2">
                            {isConnected && address ? (
                                <>
                                    <ConnectionStatusBadge className="hidden md:flex" />
                                    <NotificationBell />

                                    {/* Wallet dropdown (desktop) */}
                                    <div
                                        className="relative hidden md:block"
                                        onClick={(e) => e.stopPropagation()}
                                    >
                                        <button
                                            onClick={() => setShowMenu(!showMenu)}
                                            className="flex items-center gap-2 rounded-full border px-4 py-2 transition-colors hover:bg-foreground/5"
                                        >
                                            <div className="h-2 w-2 rounded-full bg-emerald-500" />
                                            <span className="text-sm font-medium text-foreground">
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
                                                    initial={{ opacity: 0, y: 8, scale: 0.96 }}
                                                    animate={{ opacity: 1, y: 0, scale: 1 }}
                                                    exit={{ opacity: 0, y: 8, scale: 0.96 }}
                                                    transition={{ duration: 0.15 }}
                                                    className="absolute right-0 top-full mt-2 w-64 rounded-xl border bg-card p-2"
                                                >
                                                    <div className="px-3 py-2 mb-1">
                                                        <p className="text-xs text-muted-foreground mb-1">Connected wallet</p>
                                                        <p className="text-sm text-foreground break-all">
                                                            {truncateAddress(address, 10)}
                                                        </p>
                                                    </div>
                                                    <button
                                                        onClick={copyAddress}
                                                        className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted-foreground hover:bg-foreground/5 hover:text-foreground transition-colors"
                                                    >
                                                        {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                                                        {copied ? "Copied" : "Copy address"}
                                                    </button>
                                                    <button
                                                        onClick={() => { disconnect(); setShowMenu(false); }}
                                                        className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-red-600 dark:text-red-400 hover:bg-foreground/5 transition-colors"
                                                    >
                                                        <LogOut className="h-4 w-4" />
                                                        Disconnect
                                                    </button>
                                                </motion.div>
                                            )}
                                        </AnimatePresence>
                                    </div>

                                    {/* Mobile hamburger */}
                                    <button
                                        onClick={() => setMobileOpen(!mobileOpen)}
                                        className="md:hidden flex h-9 w-9 items-center justify-center rounded-full border text-muted-foreground transition-colors hover:text-foreground"
                                        aria-label="Toggle menu"
                                    >
                                        {mobileOpen ? <X className="h-4.5 w-4.5" /> : <Menu className="h-4.5 w-4.5" />}
                                    </button>
                                </>
                            ) : (
                                <Link
                                    href="/"
                                    className="inline-flex items-center justify-center rounded-full bg-foreground px-4 py-2 text-[13px] font-medium text-background transition-opacity hover:opacity-80"
                                >
                                    Connect wallet
                                </Link>
                            )}
                        </div>
                    </div>
                </div>
            </nav>

            {/* Mobile drawer */}
            <AnimatePresence>
                {mobileOpen && isConnected && (
                    <>
                        {/* Backdrop */}
                        <motion.div
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            transition={{ duration: 0.2 }}
                            className="fixed inset-0 z-40 bg-background/60 backdrop-blur-sm md:hidden"
                            onClick={() => setMobileOpen(false)}
                        />

                        {/* Drawer panel */}
                        <motion.div
                            initial={{ opacity: 0, y: -12 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -12 }}
                            transition={{ duration: 0.2, ease: "easeOut" }}
                            className="fixed left-4 right-4 top-26 z-50 md:hidden rounded-xl border bg-card overflow-hidden"
                        >
                            {/* Address */}
                            <div className="px-5 py-4 border-b">
                                <p className="text-xs text-muted-foreground mb-1">Wallet</p>
                                <div className="flex items-center justify-between">
                                    <div className="flex items-center gap-2">
                                        <div className="h-2 w-2 rounded-full bg-emerald-500" />
                                        <span className="text-sm text-foreground">
                                            {address ? truncateAddress(address, 8) : ""}
                                        </span>
                                    </div>
                                    <button onClick={copyAddress} aria-label="Copy address" className="text-muted-foreground hover:text-foreground transition-colors">
                                        {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                                    </button>
                                </div>
                            </div>

                            {/* Nav links */}
                            <div className="px-3 py-3 space-y-0.5">
                                {NAV_LINKS.map((item) => (
                                    <Link
                                        key={item.label}
                                        href={item.href}
                                        className={cn(
                                            "flex items-center justify-between rounded-xl px-4 py-3 text-sm font-medium transition-colors",
                                            isActive(item.href)
                                                ? "bg-foreground text-background"
                                                : "text-muted-foreground hover:bg-foreground/5 hover:text-foreground"
                                        )}
                                    >
                                        {item.label}
                                    </Link>
                                ))}
                            </div>

                            {/* Disconnect */}
                            <div className="px-3 pb-3 pt-1 border-t mt-1">
                                <button
                                    onClick={() => { disconnect(); setMobileOpen(false); }}
                                    className="flex w-full items-center gap-3 rounded-xl px-4 py-3 text-sm font-medium text-red-600 dark:text-red-400 hover:bg-foreground/5 transition-colors"
                                >
                                    <LogOut className="h-4 w-4" />
                                    Disconnect
                                </button>
                            </div>
                        </motion.div>
                    </>
                )}
            </AnimatePresence>
        </>
    );
}
