"use client";

import { motion } from "framer-motion";
import { cn } from "@/lib/utils";

const pulse = {
    opacity: [0.45, 0.85, 0.45],
};

const pulseTransition = {
    duration: 1.35,
    repeat: Infinity,
    ease: "easeInOut" as const,
};

export function SkeletonLine({
    className,
    width = "100%",
    height = "0.75rem",
}: {
    className?: string;
    width?: string;
    height?: string;
}) {
    return (
        <motion.div
            aria-hidden
            className={cn("rounded-md bg-secondary", className)}
            style={{ width, height, minHeight: height }}
            animate={pulse}
            transition={pulseTransition}
        />
    );
}

export function SkeletonCard({
    className,
    children,
}: {
    className?: string;
    children?: React.ReactNode;
}) {
    return (
        <motion.div
            aria-hidden
            animate={pulse}
            transition={pulseTransition}
            className={cn(
                "rounded-2xl border border-border bg-secondary/40",
                className
            )}
        >
            {children}
        </motion.div>
    );
}

export function SkeletonTable({
    rows = 5,
    cols = 4,
    className,
}: {
    rows?: number;
    cols?: number;
    className?: string;
}) {
    return (
        <div
            className={cn("w-full overflow-hidden rounded-2xl border border-border", className)}
            aria-hidden
        >
            <div className="grid gap-0 border-b border-border bg-secondary/20 px-4 py-3"
                style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
                {Array.from({ length: cols }).map((_, c) => (
                    <SkeletonLine key={c} width="55%" height="0.65rem" />
                ))}
            </div>
            <div className="divide-y divide-border bg-white">
                {Array.from({ length: rows }).map((_, r) => (
                    <div
                        key={r}
                        className="grid gap-3 px-4 py-3.5"
                        style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
                    >
                        {Array.from({ length: cols }).map((__, c) => (
                            <SkeletonLine key={c} width={c === cols - 1 ? "70%" : "85%"} height="0.7rem" />
                        ))}
                    </div>
                ))}
            </div>
        </div>
    );
}

export function SkeletonChart({ className }: { className?: string }) {
    return (
        <SkeletonCard className={cn("h-48 sm:h-56 w-full p-4 sm:p-5", className)}>
            <div className="flex h-full flex-col justify-end gap-2">
                <div className="flex flex-1 items-end gap-1.5 sm:gap-2">
                    {[40, 65, 45, 80, 55, 90, 70, 50].map((h, i) => (
                        <motion.div
                            key={i}
                            className="flex-1 rounded-t-md bg-muted/80"
                            style={{ height: `${h}%` }}
                            animate={pulse}
                            transition={{ ...pulseTransition, delay: i * 0.05 }}
                        />
                    ))}
                </div>
                <SkeletonLine width="100%" height="0.5rem" className="opacity-60" />
            </div>
        </SkeletonCard>
    );
}
