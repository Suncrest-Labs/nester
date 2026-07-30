"use client";

import { useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { ShieldCheck, User, Bell, Globe, Monitor } from "lucide-react";
import { AppShell } from "@/components/app-shell";
import { useWallet } from "@/components/wallet-provider";
import { KYCSection, type KYCStatus } from "@/components/kyc/KYCSection";
import { BankAccountsSection } from "@/components/settings/bank-accounts-section";
import { SessionsSection } from "@/components/settings/sessions-section";
import { cn } from "@/lib/utils";
import { useLocale, useTranslations } from "@/context/locale-context";
import { SUPPORTED_LOCALES, LOCALE_LABELS, type Locale } from "@/lib/i18n/config";

// --- Savings goal notification settings (#740) ---

type Channel = "push" | "email" | "in_app";
type Milestone = "25" | "50" | "75" | "100" | "deadline";

const CHANNELS: { id: Channel; label: string }[] = [
    { id: "push", label: "Push" },
    { id: "email", label: "Email" },
    { id: "in_app", label: "In-App" },
];

const MILESTONES: { id: Milestone; label: string }[] = [
    { id: "25", label: "25% reached" },
    { id: "50", label: "50% reached" },
    { id: "75", label: "75% reached" },
    { id: "100", label: "Goal completed" },
    { id: "deadline", label: "Deadline breach" },
];

type SavingsNotifPrefs = Record<Milestone, Record<Channel, boolean>>;

function defaultSavingsPrefs(): SavingsNotifPrefs {
    const prefs = {} as SavingsNotifPrefs;
    for (const m of MILESTONES) {
        prefs[m.id] = { push: true, email: true, in_app: true };
    }
    return prefs;
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: () => void }) {
    return (
        <label className="relative inline-flex cursor-pointer items-center">
            <input type="checkbox" checked={checked} onChange={onChange} className="peer sr-only" />
            <div className="h-5 w-9 rounded-full bg-black/10 dark:bg-white/10 peer-checked:bg-black dark:peer-checked:bg-blue-600 transition-colors after:absolute after:left-0.5 after:top-0.5 after:h-4 after:w-4 after:rounded-full after:bg-white after:transition-transform peer-checked:after:translate-x-4" />
        </label>
    );
}

function SavingsNotificationsSection() {
    const [prefs, setPrefs] = useState<SavingsNotifPrefs>(defaultSavingsPrefs);
    const [saving, setSaving] = useState(false);
    const [saved, setSaved] = useState(false);

    const toggle = useCallback((milestone: Milestone, channel: Channel) => {
        setPrefs((prev) => ({
            ...prev,
            [milestone]: { ...prev[milestone], [channel]: !prev[milestone][channel] },
        }));
        setSaved(false);
    }, []);

    const save = useCallback(async () => {
        setSaving(true);
        try {
            await fetch("/api/v1/notifications/preferences", {
                method: "PATCH",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ savings_milestones: prefs }),
            });
            setSaved(true);
        } finally {
            setSaving(false);
        }
    }, [prefs]);

    return (
        <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-6">
            <h2 className="mb-1 text-sm font-medium text-black dark:text-white">Savings Goal Milestones</h2>
            <p className="mb-5 text-xs text-black/40 dark:text-white/40">Choose which channels receive alerts at each milestone</p>

            <div className="overflow-x-auto">
                <table className="w-full text-sm">
                    <thead>
                        <tr>
                            <th className="pb-3 text-left text-xs font-medium text-black/40 dark:text-white/40 w-40">Milestone</th>
                            {CHANNELS.map((c) => (
                                <th key={c.id} className="pb-3 text-center text-xs font-medium text-black/40 dark:text-white/40 w-20">
                                    {c.label}
                                </th>
                            ))}
                        </tr>
                    </thead>
                    <tbody className="divide-y divide-black/4 dark:divide-white/4">
                        {MILESTONES.map((m) => (
                            <tr key={m.id}>
                                <td className="py-3 text-sm text-black dark:text-white">{m.label}</td>
                                {CHANNELS.map((c) => (
                                    <td key={c.id} className="py-3 text-center">
                                        <div className="flex justify-center">
                                            <Toggle
                                                checked={prefs[m.id][c.id]}
                                                onChange={() => toggle(m.id, c.id)}
                                            />
                                        </div>
                                    </td>
                                ))}
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>

            <div className="mt-5 flex items-center gap-3">
                <button
                    onClick={save}
                    disabled={saving}
                    className="rounded-xl bg-black dark:bg-blue-600 px-5 py-2.5 text-sm text-white transition-opacity hover:opacity-75 disabled:opacity-40"
                >
                    {saving ? "Saving…" : "Save"}
                </button>
                {saved && <span className="text-xs text-black/40 dark:text-white/40">Saved</span>}
            </div>
        </div>
    );
}

type Tab = "profile" | "verification" | "security" | "notifications" | "preferences";

const TABS: { id: Tab; label: string; icon: React.ElementType }[] = [
    { id: "profile", label: "Profile", icon: User },
    { id: "verification", label: "Verification", icon: ShieldCheck },
    { id: "security", label: "Security", icon: Monitor },
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
    const { locale, setLocale } = useLocale();
    const t = useTranslations();

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
                <h1 className="text-2xl text-black dark:text-white sm:text-3xl">Settings</h1>
                <p className="mt-1 text-sm text-black/40 dark:text-white/40">Manage your account and preferences</p>
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
                                            ? "bg-black/[0.04] dark:bg-white/[0.04] text-black dark:text-white font-medium"
                                            : "text-black/40 dark:text-white/40 hover:bg-black/[0.02] dark:hover:bg-white/[0.02] hover:text-black/60 dark:hover:text-white/60"
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
                                className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-6"
                            >
                                <h2 className="mb-5 text-sm font-medium text-black dark:text-white">Profile</h2>
                                <div className="space-y-4">
                                    <div>
                                        <label className="mb-1.5 block text-xs text-black/45 dark:text-white/45">Wallet Address</label>
                                        <div className="h-11 rounded-xl border border-black/8 dark:border-white/8 bg-black/[0.02] dark:bg-white/[0.02] px-4 flex items-center">
                                            <span className="font-mono text-sm text-black/50 dark:text-white/50 truncate">{address}</span>
                                        </div>
                                    </div>
                                    <div>
                                        <label className="mb-1.5 block text-xs text-black/45 dark:text-white/45">Display Name</label>
                                        <input
                                            type="text"
                                            placeholder="Enter a display name"
                                            className="h-11 w-full rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] px-4 text-sm outline-none transition-colors focus:border-black/25 dark:focus:border-white/25 focus:bg-white dark:focus:bg-[#100F0F]"
                                        />
                                    </div>
                                    <button className="rounded-xl bg-black dark:bg-blue-600 px-5 py-2.5 text-sm text-white transition-opacity hover:opacity-75">
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
                                className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-6"
                            >
                                <h2 className="mb-5 text-sm font-medium text-black dark:text-white">Identity Verification</h2>
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

                        {activeTab === "security" && (
                            <motion.div
                                key="security"
                                initial={{ opacity: 0, y: 8 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 8 }}
                            >
                                <SessionsSection />
                            </motion.div>
                        )}

                        {activeTab === "notifications" && (
                            <motion.div
                                key="notifications"
                                initial={{ opacity: 0, y: 8 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 8 }}
                                className="space-y-6"
                            >
                                <div className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-6">
                                    <h2 className="mb-5 text-sm font-medium text-black dark:text-white">Notification Preferences</h2>
                                    <div className="space-y-4">
                                        {[
                                            { label: "Deposit confirmed", desc: "When a deposit is confirmed on-chain" },
                                            { label: "Withdrawal processed", desc: "When a fiat withdrawal is settled" },
                                            { label: "KYC status update", desc: "When your verification status changes" },
                                            { label: "Yield accrual", desc: "Daily yield summary" },
                                        ].map((item) => (
                                            <div key={item.label} className="flex items-center justify-between gap-4">
                                                <div>
                                                    <p className="text-sm text-black dark:text-white">{item.label}</p>
                                                    <p className="text-xs text-black/40 dark:text-white/40">{item.desc}</p>
                                                </div>
                                                <label className="relative inline-flex cursor-pointer items-center">
                                                    <input type="checkbox" defaultChecked className="peer sr-only" />
                                                    <div className="h-5 w-9 rounded-full bg-black/10 dark:bg-white/10 peer-checked:bg-black dark:peer-checked:bg-blue-600 transition-colors after:absolute after:left-0.5 after:top-0.5 after:h-4 after:w-4 after:rounded-full after:bg-white after:transition-transform peer-checked:after:translate-x-4" />
                                                </label>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                                <SavingsNotificationsSection />
                            </motion.div>
                        )}

                        {activeTab === "preferences" && (
                            <motion.div
                                key="preferences"
                                initial={{ opacity: 0, y: 8 }}
                                animate={{ opacity: 1, y: 0 }}
                                exit={{ opacity: 0, y: 8 }}
                                className="rounded-2xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] p-6"
                            >
                                <h2 className="mb-5 text-sm font-medium text-black dark:text-white">Preferences</h2>
                                <div className="space-y-4">
                                    <div>
                                        <label className="mb-1.5 block text-xs text-black/45 dark:text-white/45">Currency Display</label>
                                        <select className="h-11 w-full rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] px-4 text-sm outline-none appearance-none focus:border-black/25 dark:focus:border-white/25">
                                            <option value="USD">USD ($)</option>
                                            <option value="EUR">EUR (€)</option>
                                            <option value="GBP">GBP (£)</option>
                                            <option value="NGN">NGN (₦)</option>
                                        </select>
                                    </div>
                                    <div>
                                        <label
                                            htmlFor="locale-select"
                                            className="mb-1.5 block text-xs text-black/45 dark:text-white/45"
                                        >
                                            {t("settings.language")}
                                        </label>
                                        <select
                                            id="locale-select"
                                            value={locale}
                                            onChange={(e) => setLocale(e.target.value as Locale)}
                                            className="h-11 w-full rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.02] dark:bg-white/[0.02] px-4 text-sm outline-none appearance-none focus:border-black/25 dark:focus:border-white/25"
                                        >
                                            {SUPPORTED_LOCALES.map((l) => (
                                                <option key={l} value={l}>
                                                    {LOCALE_LABELS[l]}
                                                </option>
                                            ))}
                                        </select>
                                        <p className="mt-1.5 text-xs text-black/40 dark:text-white/40">
                                            {t("settings.languageDescription")}
                                        </p>
                                    </div>
                                    <BankAccountsSection />
                                </div>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>
            </div>
        </AppShell>
    );
}
