"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { ShieldCheck, User, Bell, Globe } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { KYCSection, type KYCStatus } from "@/components/kyc/KYCSection";
import { cn } from "@/lib/utils";

type Tab = "profile" | "verification" | "notifications" | "preferences";

const TABS: { id: Tab; label: string; icon: React.ElementType }[] = [
    { id: "profile", label: "Profile", icon: User },
    { id: "verification", label: "Verification", icon: ShieldCheck },
    { id: "notifications", label: "Notifications", icon: Bell },
    { id: "preferences", label: "Preferences", icon: Globe },
];

// Mocked KYC state — in a real app this would come from an API call
function useKYCState() {
    const [status, setStatus] = useState<KYCStatus>("unverified");
    const [submittedAt, setSubmittedAt] = useState<string | null>(null);
    const [reviewedAt] = useState<string | null>(null);
    const [rejectionReason] = useState<string | null>(null);
    const [isSubmitting, setIsSubmitting] = useState(false);

    const submitKYC = async (formData: FormData) => {
        setIsSubmitting(true);
        try {
            // In a real app, POST /api/v1/users/{userId}/kyc
            await new Promise((r) => setTimeout(r, 1200));
            setStatus("pending");
            setSubmittedAt(new Date().toISOString());
        } finally {
            setIsSubmitting(false);
        }
    };

    return { status, submittedAt, reviewedAt, rejectionReason, isSubmitting, submitKYC };
}

export default function SettingsPage() {
    const { isConnected, address } = useWallet();
    const router = useRouter();
    const [activeTab, setActiveTab] = useState<Tab>("profile");

    const kyc = useKYCState();

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    return (
        <AppShell>
            <motion.div
                initial={{ opacity: 0, y: -8 }}
                animate={{ opacity: 1, y: 0 }}
                className="mb-8"
            >
                <h1 className="text-2xl text-black sm:text-3xl">Settings</h1>
                <p className="mt-1 text-sm text-black/40">Manage your account and preferences</p>
            </motion.div>

            <div className="flex flex-col gap-8 lg:flex-row lg:gap-10">
                {/* Sidebar tabs */}
                <aside className="lg:w-52 shrink-0">
                    <nav className="space-y-0.5">
                        {TABS.map((tab) => {
                            const Icon = tab.icon;
                            return (
                                <button
                                    key={tab.id}
                                    onClick={() => setActiveTab(tab.id)}
                                    className={cn(
                                        "flex w-full items-center gap-3 rounded-xl px-4 py-2.5 text-sm transition-colors text-left",
                                        activeTab === tab.id
                                            ? "bg-black/[0.04] text-black font-medium"
                                            : "text-black/40 hover:bg-black/[0.02] hover:text-black/60"
                                    )}
                                >
                                    <Icon className="h-4 w-4 shrink-0" />
                                    {tab.label}
                                </button>
                            );
                        })}
                    </nav>
                </aside>

                {/* Tab content */}
                <div className="flex-1 min-w-0">
                    <AnimatePresence mode="wait">
                        {activeTab === "profile" && (
                            <motion.div
                                key="profile"
                                initial={{ opacity: 0, y: 8 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 8 }}
                                className="rounded-2xl border border-black/8 bg-white p-6"
                            >
                                <h2 className="mb-5 text-sm font-medium text-black">Profile</h2>
                                <div className="space-y-4">
                                    <div>
                                        <label className="mb-1.5 block text-xs text-black/45">Wallet Address</label>
                                        <div className="h-11 rounded-xl border border-black/8 bg-black/[0.02] px-4 flex items-center">
                                            <span className="font-mono text-sm text-black/50 truncate">{address}</span>
                                        </div>
                                    </div>
                                    <div>
                                        <label className="mb-1.5 block text-xs text-black/45">Display Name</label>
                                        <input
                                            type="text"
                                            placeholder="Enter a display name"
                                            className="h-11 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 text-sm outline-none transition-colors focus:border-black/25 focus:bg-white"
                                        />
                                    </div>
                                    <button className="rounded-xl bg-black px-5 py-2.5 text-sm text-white transition-opacity hover:opacity-75">
                                        Save Changes
                                    </button>
                                </div>
                            </motion.div>
                        )}

                        {activeTab === "verification" && (
                            <motion.div
                                key="verification"
                                initial={{ opacity: 0, y: 8 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 8 }}
                                className="rounded-2xl border border-black/8 bg-white p-6"
                            >
                                <h2 className="mb-5 text-sm font-medium text-black">Identity Verification</h2>
                                <KYCSection
                                    status={kyc.status}
                                    submittedAt={kyc.submittedAt}
                                    reviewedAt={kyc.reviewedAt}
                                    rejectionReason={kyc.rejectionReason}
                                    onSubmit={kyc.submitKYC}
                                    isSubmitting={kyc.isSubmitting}
                                />
                            </motion.div>
                        )}

                        {activeTab === "notifications" && (
                            <motion.div
                                key="notifications"
                                initial={{ opacity: 0, y: 8 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 8 }}
                                className="rounded-2xl border border-black/8 bg-white p-6"
                            >
                                <h2 className="mb-5 text-sm font-medium text-black">Notification Preferences</h2>
                                <div className="space-y-4">
                                    {[
                                        { label: "Deposit confirmed", desc: "When a deposit is confirmed on-chain" },
                                        { label: "Withdrawal processed", desc: "When a fiat withdrawal is settled" },
                                        { label: "KYC status update", desc: "When your verification status changes" },
                                        { label: "Yield accrual", desc: "Daily yield summary" },
                                    ].map((item) => (
                                        <div key={item.label} className="flex items-center justify-between gap-4">
                                            <div>
                                                <p className="text-sm text-black">{item.label}</p>
                                                <p className="text-xs text-black/40">{item.desc}</p>
                                            </div>
                                            <label className="relative inline-flex cursor-pointer items-center">
                                                <input type="checkbox" defaultChecked className="peer sr-only" />
                                                <div className="h-5 w-9 rounded-full bg-black/10 peer-checked:bg-black transition-colors after:absolute after:left-0.5 after:top-0.5 after:h-4 after:w-4 after:rounded-full after:bg-white after:transition-transform peer-checked:after:translate-x-4" />
                                            </label>
                                        </div>
                                    ))}
                                </div>
                            </motion.div>
                        )}

                        {activeTab === "preferences" && (
                            <motion.div
                                key="preferences"
                                initial={{ opacity: 0, y: 8 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 8 }}
                                className="rounded-2xl border border-black/8 bg-white p-6"
                            >
                                <h2 className="mb-5 text-sm font-medium text-black">Preferences</h2>
                                <div className="space-y-4">
                                    <div>
                                        <label className="mb-1.5 block text-xs text-black/45">Currency Display</label>
                                        <select className="h-11 w-full rounded-xl border border-black/10 bg-black/[0.02] px-4 text-sm outline-none appearance-none focus:border-black/25">
                                            <option value="USD">USD ($)</option>
                                            <option value="EUR">EUR (€)</option>
                                            <option value="GBP">GBP (£)</option>
                                            <option value="NGN">NGN (₦)</option>
                                        </select>
                                    </div>
                                </div>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>
            </div>
        </AppShell>
    );
}
