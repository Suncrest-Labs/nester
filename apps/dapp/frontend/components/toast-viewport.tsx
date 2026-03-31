"use client";

import { AnimatePresence, motion } from "framer-motion";
import { CheckCircle2, Info, AlertTriangle, XCircle, X } from "lucide-react";
import { useToast, type ToastVariant } from "@/components/toast-provider";
import { cn } from "@/lib/utils";

const variantStyles: Record<
    ToastVariant,
    { icon: typeof CheckCircle2; bar: string; iconClass: string }
> = {
    success: {
        icon: CheckCircle2,
        bar: "bg-emerald-500",
        iconClass: "text-emerald-600",
    },
    error: {
        icon: XCircle,
        bar: "bg-rose-500",
        iconClass: "text-rose-600",
    },
    warning: {
        icon: AlertTriangle,
        bar: "bg-amber-500",
        iconClass: "text-amber-600",
    },
    info: {
        icon: Info,
        bar: "bg-primary",
        iconClass: "text-primary",
    },
};

export function ToastViewport() {
    const { toasts, dismiss } = useToast();

    return (
        <div
            className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-[min(92vw,22rem)] flex-col gap-2 sm:bottom-6 sm:right-6"
            aria-live="polite"
        >
            <AnimatePresence mode="popLayout">
                {toasts.map((toast) => {
                    const cfg = variantStyles[toast.variant];
                    const Icon = cfg.icon;
                    return (
                        <motion.div
                            key={toast.id}
                            layout
                            initial={{ opacity: 0, y: 16, scale: 0.97 }}
                            animate={{ opacity: 1, y: 0, scale: 1 }}
                            exit={{ opacity: 0, x: 12, scale: 0.96 }}
                            transition={{ type: "spring", stiffness: 420, damping: 32 }}
                            className="pointer-events-auto overflow-hidden rounded-2xl border border-border bg-white shadow-xl shadow-black/10"
                        >
                            <div className={cn("h-0.5 w-full", cfg.bar)} />
                            <div className="flex gap-3 p-3.5 sm:p-4">
                                <Icon className={cn("mt-0.5 h-5 w-5 shrink-0", cfg.iconClass)} />
                                <div className="min-w-0 flex-1">
                                    <p className="text-sm font-medium text-foreground">
                                        {toast.title}
                                    </p>
                                    {toast.description ? (
                                        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                                            {toast.description}
                                        </p>
                                    ) : null}
                                    {toast.action ? (
                                        <div className="mt-2.5">
                                            {toast.action.href ? (
                                                <a
                                                    href={toast.action.href}
                                                    target="_blank"
                                                    rel="noreferrer"
                                                    className="inline-flex min-h-9 items-center rounded-full border border-border px-3 py-1.5 text-xs font-medium text-foreground/75 transition-colors hover:bg-secondary hover:text-foreground"
                                                >
                                                    {toast.action.label}
                                                </a>
                                            ) : (
                                                <button
                                                    type="button"
                                                    onClick={toast.action.onClick}
                                                    className="inline-flex min-h-9 items-center rounded-full border border-border px-3 py-1.5 text-xs font-medium text-foreground/75 transition-colors hover:bg-secondary hover:text-foreground"
                                                >
                                                    {toast.action.label}
                                                </button>
                                            )}
                                        </div>
                                    ) : null}
                                </div>
                                <button
                                    type="button"
                                    onClick={() => dismiss(toast.id)}
                                    className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
                                    aria-label="Dismiss"
                                >
                                    <X className="h-3.5 w-3.5" />
                                </button>
                            </div>
                        </motion.div>
                    );
                })}
            </AnimatePresence>
        </div>
    );
}
