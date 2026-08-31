"use client";

import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ShieldAlert, X } from "lucide-react";
import { useNotifications } from "@/components/notifications-provider";
import { NotificationActionLink } from "@/components/notification-action-link";
import { cn } from "@/lib/utils";

export function NotificationsToaster() {
    const { toasts, dismissToast } = useNotifications();
    const shouldReduceMotion = useReducedMotion();

    return (
        <div className="pointer-events-none fixed right-4 top-20 z-70 flex w-[min(92vw,24rem)] flex-col gap-2">
            <AnimatePresence>
                {toasts.map((toast) => {
                    const isSafety = toast.priority === "safety";

                    return (
                        <motion.div
                            key={toast.id}
                            role={isSafety ? "alert" : "status"}
                            aria-live={isSafety ? "assertive" : "polite"}
                            aria-atomic="true"
                            initial={
                                shouldReduceMotion
                                    ? { opacity: 0 }
                                    : { opacity: 0, x: 24, scale: 0.96 }
                            }
                            animate={{ opacity: 1, x: 0, scale: 1 }}
                            exit={
                                shouldReduceMotion
                                    ? { opacity: 0 }
                                    : { opacity: 0, x: 24, scale: 0.96 }
                            }
                            transition={{ duration: 0.2 }}
                            className={cn(
                                "pointer-events-auto rounded-2xl border p-4 shadow-xl shadow-black/8 transition-colors",
                                isSafety
                                    ? "border-red-500/40 bg-red-50/95 dark:bg-red-950/80 dark:border-red-500/50"
                                    : "border-border bg-white dark:bg-[#100F0F]"
                            )}
                        >
                            <div className="flex items-start justify-between gap-3">
                                <div className="flex gap-2.5">
                                    {isSafety && (
                                        <div className="mt-0.5 shrink-0 rounded-full bg-red-100 p-1 dark:bg-red-900/60">
                                            <ShieldAlert className="h-4 w-4 text-red-600 dark:text-red-400" />
                                        </div>
                                    )}
                                    <div>
                                        <div className="flex items-center gap-1.5">
                                            {isSafety && (
                                                <span className="rounded bg-red-600 px-1.5 py-0.5 text-[10px] font-bold tracking-wide text-white uppercase">
                                                    CRITICAL SAFETY
                                                </span>
                                            )}
                                            <p
                                                className={cn(
                                                    "text-sm font-medium",
                                                    isSafety
                                                        ? "text-red-950 dark:text-red-100"
                                                        : "text-foreground"
                                                )}
                                            >
                                                {toast.title}
                                            </p>
                                        </div>
                                        <p
                                            className={cn(
                                                "mt-1 text-xs leading-relaxed",
                                                isSafety
                                                    ? "text-red-900/90 dark:text-red-200/90"
                                                    : "text-muted-foreground"
                                            )}
                                        >
                                            {toast.message}
                                        </p>
                                    </div>
                                </div>
                                <button
                                    onClick={() => dismissToast(toast.id)}
                                    className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                                    aria-label={`Dismiss ${toast.title}`}
                                >
                                    <X className="h-3.5 w-3.5" />
                                </button>
                            </div>

                            {toast.actionUrl && (
                                <div className="mt-3 flex items-center justify-end">
                                    <NotificationActionLink
                                        href={toast.actionUrl}
                                        className={cn(
                                            "inline-flex items-center rounded-full px-3 py-1.5 text-xs font-medium transition-colors",
                                            isSafety
                                                ? "bg-red-600 text-white hover:bg-red-700 dark:bg-red-500 dark:hover:bg-red-600"
                                                : "border border-border bg-background px-3 py-1.5 text-foreground/80 hover:bg-secondary"
                                        )}
                                    >
                                        {toast.actionLabel || "View Details"}
                                    </NotificationActionLink>
                                </div>
                            )}
                        </motion.div>
                    );
                })}
            </AnimatePresence>
        </div>
    );
}
