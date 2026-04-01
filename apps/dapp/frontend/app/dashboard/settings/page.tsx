/**
 * SETTINGS PAGE — Temporarily disabled.
 * Replaced by the Portfolio Tracker page at /dashboard/portfolio.
 * Uncomment the export below and remove the redirect to restore.
 */
"use client";

// ─── Active: redirect to Portfolio ───────────────────────────────────────────
export { default } from "@/app/dashboard/portfolio/page";

/*
// ─── Disabled settings page (kept for reference) ────────────────────────────

import { useWallet } from "@/components/wallet-provider";
import { Navbar } from "@/components/navbar";
import { useRouter } from "next/navigation";
import React, { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { cn } from "@/lib/utils";
import {
    Wallet,
    Bell,
    Shield,
    Monitor,
    LogOut,
    Copy,
    Check,
    Smartphone,
    Laptop,
} from "lucide-react";

// Types
type Currency = "USD" | "GBP" | "EUR" | "NGN";
type NotificationSettings = {
    confirmations: boolean;
    alerts: boolean;
    rebalancing: boolean;
    marketSummary: boolean;
};

import { useSettings } from "@/context/settings-context";
*/
export default function SettingsPage() {
    const { isConnected, address, disconnect } = useWallet();
    const { currency, setCurrency } = useSettings();
    const router = useRouter();
    const [copied, setCopied] = useState(false);

    // Preferences State with LocalStorage
    const [notifications, setNotifications] = useState<NotificationSettings>({
        confirmations: true,
        alerts: true,
        rebalancing: true,
        marketSummary: false,
    });
    const [autoDisconnect, setAutoDisconnect] = useState("30");

    useEffect(() => {
        if (!isConnected) {
            router.push("/");
        }
    }, [isConnected, router]);

    // Load from LocalStorage
    useEffect(() => {
        const savedNotifications = localStorage.getItem("nester_notifications");
        const savedTimeout = localStorage.getItem("nester_timeout");

        if (savedNotifications) {
            try {
                setNotifications(JSON.parse(savedNotifications));
            } catch (e) {
                console.error("Failed to parse notifications", e);
            }
        }
        if (savedTimeout) setAutoDisconnect(savedTimeout);
    }, []);

    // Save helpers
    const toggleNotification = (key: keyof NotificationSettings) => {
        const updated = { ...notifications, [key]: !notifications[key] };
        setNotifications(updated);
        localStorage.setItem("nester_notifications", JSON.stringify(updated));
    };

    const updateTimeout = (val: string) => {
        setAutoDisconnect(val);
        localStorage.setItem("nester_timeout", val);
    };

    const copyAddress = () => {
        if (address) {
            navigator.clipboard.writeText(address);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        }
    };

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-background text-foreground selection:bg-emerald-100 selection:text-emerald-900">
            <Navbar />

            <main className="mx-auto max-w-[1536px] px-4 md:px-8 lg:px-12 xl:px-16 pt-36 pb-16">
                <div className="max-w-4xl">
                    {/* Header */}
                    <motion.div
                        initial={{ opacity: 0, x: -20 }}
                        animate={{ opacity: 1, x: 0 }}
                        transition={{ duration: 0.5 }}
                        className="mb-10"
                    >
                        <h1 className="font-heading text-2xl font-light text-foreground sm:text-3xl">
                            Settings
                        </h1>
                        <p className="mt-1 text-muted-foreground font-display text-sm opacity-60">
                            Configure your wallet, display preferences, and notifications.
                        </p>
                    </motion.div>

                    <div className="grid gap-8">
                        {/* Wallet Section */}
                        <SettingsSection
                            title="Wallet Management"
                            icon={Wallet}
                            delay={0.1}
                        >
                            <div className="rounded-2xl border border-border bg-white p-6 shadow-sm">
                                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                                    <div className="flex flex-col gap-1.5 flex-1 max-w-full overflow-hidden">
                                        <span className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider">
                                            Connected Address
                                        </span>
                                        <div className="flex items-center gap-3">
                                            <code className="text-sm font-mono text-foreground bg-secondary/30 px-3 py-1.5 rounded-lg break-all lg:break-normal">
                                                {address}
                                            </code>
                                            <button
                                                onClick={copyAddress}
                                                className="shrink-0 h-9 w-9 flex items-center justify-center rounded-xl border border-border hover:bg-secondary transition-all active:scale-95 group"
                                            >
                                                {copied ? (
                                                    <Check className="h-4 w-4 text-emerald-500" />
                                                ) : (
                                                    <Copy className="h-4 w-4 text-muted-foreground group-hover:text-foreground transition-colors" />
                                                )}
                                            </button>
                                        </div>
                                    </div>

                                    <button
                                        onClick={disconnect}
                                        className="inline-flex items-center justify-center gap-2 rounded-xl bg-rose-50 border border-rose-100 px-6 py-2.5 text-xs font-bold text-rose-600 transition-all hover:bg-rose-100 active:scale-95"
                                    >
                                        <LogOut className="h-3.5 w-3.5" />
                                        Disconnect Wallet
                                    </button>
                                </div>

                                <div className="mt-8 pt-6 border-t border-border/50">
                                    <p className="text-xs text-muted-foreground leading-relaxed flex items-center gap-2">
                                        <span className="inline-block h-1 w-1 rounded-full bg-emerald-500" />
                                        Current network: <span className="font-semibold text-foreground">Stellar Public Network</span>
                                    </p>
                                </div>
                            </div>
                        </SettingsSection>

                        {/* Display Section */}
                        <SettingsSection
                            title="Display Preferences"
                            icon={Monitor}
                            delay={0.2}
                        >
                            <div className="rounded-2xl border border-border bg-white p-6 shadow-sm space-y-8">
                                <div className="flex items-center justify-between">
                                    <div className="space-y-1">
                                        <p className="text-sm font-medium text-foreground">Primary Currency</p>
                                        <p className="text-xs text-muted-foreground">The default currency for portfolio valuation.</p>
                                    </div>
                                    <div className="flex bg-secondary/30 p-1 rounded-xl border border-border/50">
                                        {(["USD", "GBP", "EUR", "NGN"] as Currency[]).map((cur) => (
                                            <button
                                                key={cur}
                                                onClick={() => setCurrency(cur)}
                                                className={cn(
                                                    "px-3 py-1.5 rounded-lg text-[10px] font-bold transition-all",
                                                    currency === cur
                                                        ? "bg-white text-foreground shadow-sm border border-border/50"
                                                        : "text-muted-foreground hover:text-foreground"
                                                )}
                                            >
                                                {cur}
                                            </button>
                                        ))}
                                    </div>
                                </div>

                                <div className="flex items-center justify-between opacity-50">
                                    <div className="space-y-1">
                                        <p className="text-sm font-medium text-foreground">Interface Theme</p>
                                        <p className="text-xs text-muted-foreground italic">Dark mode is coming soon.</p>
                                    </div>
                                    <div className="flex bg-secondary/30 p-1 rounded-xl border border-border/20 cursor-not-allowed">
                                        <button className="px-4 py-1.5 rounded-lg text-[10px] font-bold bg-white text-foreground shadow-sm border border-border/50">
                                            Light
                                        </button>
                                        <button className="px-4 py-1.5 rounded-lg text-[10px] font-bold text-muted-foreground/30">
                                            Dark
                                        </button>
                                    </div>
                                </div>
                            </div>
                        </SettingsSection>

                        {/* Notifications Section */}
                        <SettingsSection
                            title="Notification Preferences"
                            icon={Bell}
                            delay={0.3}
                        >
                            <div className="rounded-2xl border border-border bg-white divide-y divide-border/50 shadow-sm overflow-hidden">
                                <ToggleItem
                                    label="Confirmations"
                                    description="Receive immediate alerts for deposit and withdrawal events."
                                    active={notifications.confirmations}
                                    onToggle={() => toggleNotification("confirmations")}
                                />
                                <ToggleItem
                                    label="Prometheus Portfolio Alerts"
                                    description="Get notified about AI-driven rebalancing opportunities and risk warnings."
                                    active={notifications.alerts}
                                    onToggle={() => toggleNotification("alerts")}
                                    highlight
                                />
                                <ToggleItem
                                    label="Vault Rebalancing"
                                    description="Alerts when automated yield strategy shifts occur."
                                    active={notifications.rebalancing}
                                    onToggle={() => toggleNotification("rebalancing")}
                                />
                                <ToggleItem
                                    label="Market Summary"
                                    description="Weekly digest of DeFi performance and market insights."
                                    active={notifications.marketSummary}
                                    onToggle={() => toggleNotification("marketSummary")}
                                />
                            </div>
                        </SettingsSection>

                        {/* Security Section */}
                        <SettingsSection
                            title="Security & Sessions"
                            icon={Shield}
                            delay={0.4}
                        >
                            <div className="grid gap-6">
                                <div className="rounded-2xl border border-border bg-white p-6 shadow-sm">
                                    <div className="flex items-center justify-between mb-8">
                                        <div className="space-y-1">
                                            <p className="text-sm font-medium text-foreground">Auto-disconnect Timeout</p>
                                            <p className="text-xs text-muted-foreground">Duration of inactivity before the vault is locked.</p>
                                        </div>
                                        <div className="flex items-center gap-3">
                                            <input
                                                type="number"
                                                value={autoDisconnect}
                                                onChange={(e) => updateTimeout(e.target.value)}
                                                className="w-16 rounded-xl border border-border bg-white px-3 py-2 text-sm font-mono focus:border-emerald-500 focus:outline-none transition-colors"
                                            />
                                            <span className="text-[10px] font-bold text-muted-foreground uppercase tracking-widest">Min</span>
                                        </div>
                                    </div>

                                    <div className="space-y-4">
                                        <span className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider block mb-2">
                                            Active Sessions
                                        </span>

                                        <SessionItem
                                            icon={Laptop}
                                            device="MacBook Pro · London, UK"
                                            status="Current Session"
                                            current
                                        />
                                        <SessionItem
                                            icon={Smartphone}
                                            device="iPhone 15 Pro · Lagos, NG"
                                            status="Active 2 hours ago"
                                        />
                                    </div>
                                </div>
                            </div>
                        </SettingsSection>
                    </div>
                </div>
            </main>
        </div>
    );
}

// Sub-components
interface SettingsSectionProps {
    title: string;
    icon: React.ElementType;
    children: React.ReactNode;
    delay?: number;
    className?: string;
}

function SettingsSection({ title, icon: Icon, children, delay = 0, className }: SettingsSectionProps) {
    return (
        <motion.section
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay }}
            className={cn("space-y-4", className)}
        >
            <div className="flex items-center gap-2.5 px-1">
                <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-secondary/50 text-foreground/40 border border-border/20">
                    <Icon className="h-3.5 w-3.5" />
                </div>
                <h2 className="text-xs font-bold text-foreground uppercase tracking-widest">{title}</h2>
            </div>
            {children}
        </motion.section>
    );
}

interface ToggleItemProps {
    label: string;
    description: string;
    active: boolean;
    onToggle: () => void;
    highlight?: boolean;
}

function ToggleItem({ label, description, active, onToggle, highlight }: ToggleItemProps) {
    return (
        <div
            onClick={onToggle}
            className="group flex items-center justify-between p-6 hover:bg-secondary/20 transition-colors cursor-pointer"
        >
            <div className="space-y-1">
                <div className="flex items-center gap-2">
                    <p className="text-sm font-medium text-foreground">{label}</p>
                    {highlight && (
                        <div className="flex h-1.5 w-1.5 rounded-full bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.5)]" />
                    )}
                </div>
                <p className="text-xs text-muted-foreground leading-relaxed max-w-md">{description}</p>
            </div>
            <div className={cn(
                "h-6 w-10 shrink-0 rounded-full border-2 transition-all relative",
                active ? "bg-emerald-500 border-emerald-500" : "bg-white border-border group-hover:border-foreground/20"
            )}>
                <motion.div
                    animate={{ x: active ? 16 : 0 }}
                    transition={{ type: "spring", stiffness: 500, damping: 30 }}
                    className={cn(
                        "h-4 w-4 rounded-full shadow-sm absolute top-0.5 left-0.5",
                        active ? "bg-white" : "bg-muted-foreground/30"
                    )}
                />
            </div>
        </div>
    );
}

interface SessionItemProps {
    icon: React.ElementType;
    device: string;
    status: string;
    current?: boolean;
}

function SessionItem({ icon: Icon, device, status, current }: SessionItemProps) {
    return (
        <div className="flex items-center justify-between p-3.5 rounded-xl border border-border/60 bg-white hover:border-black/15 transition-all group cursor-default">
            <div className="flex items-center gap-4">
                <div className="h-10 w-10 flex items-center justify-center rounded-xl bg-secondary group-hover:bg-white border border-transparent group-hover:border-border transition-all">
                    <Icon className="h-5 w-5 text-muted-foreground group-hover:text-foreground" />
                </div>
                <div className="flex flex-col gap-0.5">
                    <p className="text-sm font-medium text-foreground group-hover:text-black transition-colors">{device}</p>
                    <p className={cn(
                        "text-[10px] font-bold uppercase tracking-wider",
                        current ? "text-emerald-500" : "text-muted-foreground/60"
                    )}>
                        {status}
                    </p>
                </div>
            </div>
            {!current && (
                <button className="text-[10px] font-bold text-muted-foreground uppercase tracking-widest opacity-0 group-hover:opacity-100 transition-opacity hover:text-rose-500">
                    Revoke
                </button>
            )}
        </div>
    );
}

─── End of disabled settings page ─────────────────────────────────────────── */
