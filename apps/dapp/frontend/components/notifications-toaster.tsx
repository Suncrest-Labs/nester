"use client";

import { AnimatePresence, motion } from "framer-motion";
import { X } from "lucide-react";
import { useNotifications } from "@/components/notifications-provider";

export function NotificationsToaster() {
    const { toasts, dismissToast } = useNotifications();

    return (
        <div className="pointer-events-none fixed right-4 top-20 z-70 flex w-[min(92vw,24rem)] flex-col gap-2">
            <AnimatePresence>
                {toasts.map((toast) => (
                    <motion.div
                        key={toast.id}
                        initial={{ opacity: 0, x: 24, scale: 0.96 }}
                        animate={{ opacity: 1, x: 0, scale: 1 }}
                        exit={{ opacity: 0, x: 24, scale: 0.96 }}
                        transition={{ duration: 0.2 }}
                        className="pointer-events-auto rounded-2xl border border-border bg-white dark:bg-[#100F0F] p-4 shadow-xl shadow-black/8"
                    >
                        <div className="flex items-start justify-between gap-3">
                            <div>
                                <p className="text-sm font-medium text-foreground">
                                    {toast.title}
                                </p>
                                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                                    {toast.message}
                                </p>
                            </div>
                            <button
                                onClick={() => dismissToast(toast.id)}
                                className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                                aria-label="Dismiss"
                            >
                                <X className="h-3.5 w-3.5" />
                            </button>
                        </div>

                        {toast.actionUrl && (
                            <div className="mt-3">
                                <a
                                    href={toast.actionUrl}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="inline-flex items-center rounded-full border border-border px-3 py-1.5 text-xs font-medium text-foreground/70 transition-colors hover:bg-secondary hover:text-foreground"
                                >
                                    {toast.actionLabel || "View"}
                                </a>
                            </div>
                        )}
                    </motion.div>
                ))}
            </AnimatePresence>
        </div>
    );
}
