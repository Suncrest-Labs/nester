"use client";

import { ProtectedRoute } from "@/components/protected-route";
import { useSettings } from "@/context/settings-context";
import { AppShell } from "@/components/app-shell";
import { motion } from "framer-motion";
import { Sun, Moon, Monitor } from "lucide-react";

export default function SettingsPage() {
    const { currency, setCurrency, theme, setTheme } = useSettings();

    const currencies = ["USD", "GBP", "EUR", "NGN"] as const;
    const themes = [
        { value: "light" as const, label: "Light", icon: Sun },
        { value: "dark" as const, label: "Dark", icon: Moon },
        { value: "system" as const, label: "System", icon: Monitor },
    ];

    return (
        <ProtectedRoute>
            <AppShell>
                <motion.div
                    initial={{ opacity: 0, y: 12 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3 }}
                    className="max-w-2xl"
                >
                    <h1 className="text-[30px] font-semibold text-black dark:text-white tracking-[-0.02em]">
                        Settings
                    </h1>
                    <p className="mt-2 text-sm text-black/50 dark:text-white/50">
                        Customize your experience
                    </p>

                    {/* Settings sections */}
                    <div className="mt-8 space-y-8">
                        {/* Currency Settings */}
                        <section className="rounded-lg border border-black/[0.1] dark:border-white/[0.1] bg-white dark:bg-gray-900 p-6">
                            <h2 className="text-lg font-semibold text-black dark:text-white mb-4">
                                Currency
                            </h2>
                            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                                {currencies.map((curr) => (
                                    <button
                                        key={curr}
                                        onClick={() => setCurrency(curr)}
                                        className={`py-2 px-3 rounded-lg font-medium text-sm transition-all ${
                                            currency === curr
                                                ? "bg-purple-500 text-white"
                                                : "bg-gray-100 dark:bg-gray-800 text-black dark:text-white hover:bg-gray-200 dark:hover:bg-gray-700"
                                        }`}
                                    >
                                        {curr}
                                    </button>
                                ))}
                            </div>
                        </section>

                        {/* Theme Settings */}
                        <section className="rounded-lg border border-black/[0.1] dark:border-white/[0.1] bg-white dark:bg-gray-900 p-6">
                            <h2 className="text-lg font-semibold text-black dark:text-white mb-4">
                                Theme
                            </h2>
                            <div className="grid grid-cols-3 gap-4">
                                {themes.map(({ value, label, icon: Icon }) => (
                                    <button
                                        key={value}
                                        onClick={() => setTheme(value)}
                                        className={`flex flex-col items-center gap-2 py-4 px-4 rounded-lg font-medium text-sm transition-all border ${
                                            theme === value
                                                ? "border-purple-500 bg-purple-50 dark:bg-purple-900/30 text-purple-600 dark:text-purple-400"
                                                : "border-black/[0.1] dark:border-white/[0.1] bg-gray-50 dark:bg-gray-800 text-black dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700"
                                        }`}
                                    >
                                        <Icon className="h-5 w-5" />
                                        {label}
                                    </button>
                                ))}
                            </div>
                            <p className="mt-3 text-xs text-black/50 dark:text-white/50">
                                {theme === "system" && "Following your device's color scheme preferences"}
                                {theme === "light" && "Always show the light theme"}
                                {theme === "dark" && "Always show the dark theme"}
                            </p>
                        </section>
                    </div>
                </motion.div>
            </AppShell>
        </ProtectedRoute>
    );
}
