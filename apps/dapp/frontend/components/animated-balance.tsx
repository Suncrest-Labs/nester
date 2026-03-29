"use client";

import { useEffect, useRef, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";

interface AnimatedBalanceProps {
    /** The current (target) value to animate to */
    value: number;
    /** The previous value to animate from — drives the color flash */
    previousValue?: number;
    /** Animation duration in milliseconds (default: 1000) */
    duration?: number;
    /** Flash green for increase, red for decrease (default: true) */
    highlightChange?: boolean;
    /** Optional formatter; defaults to 2 decimal-place USD string */
    format?: (value: number) => string;
    className?: string;
}

function easeOutExpo(t: number): number {
    return t === 1 ? 1 : 1 - Math.pow(2, -10 * t);
}

function defaultFormat(value: number): string {
    return `$${value.toLocaleString("en-US", {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
    })}`;
}

/**
 * AnimatedBalance
 *
 * Smoothly counts from previousValue → value using requestAnimationFrame.
 * When `highlightChange` is true the text briefly flashes green (increase)
 * or red (decrease) before fading to the default foreground colour.
 *
 * Built on framer-motion (already a project dependency) and vanilla RAF —
 * zero new dependencies.
 */
export function AnimatedBalance({
    value,
    previousValue,
    duration = 1000,
    highlightChange = true,
    format = defaultFormat,
    className,
}: AnimatedBalanceProps) {
    const [displayValue, setDisplayValue] = useState(previousValue ?? value);
    const [flash, setFlash] = useState<"increase" | "decrease" | null>(null);
    const rafRef = useRef<number | null>(null);
    const startTimeRef = useRef<number | null>(null);
    const startValueRef = useRef(previousValue ?? value);

    useEffect(() => {
        const from = startValueRef.current;
        const to = value;

        if (from === to) return;

        // Determine direction for color flash
        if (highlightChange) {
            setFlash(to > from ? "increase" : "decrease");
        }

        // Cancel any running animation
        if (rafRef.current !== null) {
            cancelAnimationFrame(rafRef.current);
        }
        startTimeRef.current = null;

        const animate = (timestamp: number) => {
            if (startTimeRef.current === null) {
                startTimeRef.current = timestamp;
            }
            const elapsed = timestamp - startTimeRef.current;
            const progress = Math.min(elapsed / duration, 1);
            const eased = easeOutExpo(progress);

            setDisplayValue(from + (to - from) * eased);

            if (progress < 1) {
                rafRef.current = requestAnimationFrame(animate);
            } else {
                startValueRef.current = to;
                // Fade out flash after a short highlight window
                const flashTimer = setTimeout(() => setFlash(null), 600);
                return () => clearTimeout(flashTimer);
            }
        };

        rafRef.current = requestAnimationFrame(animate);

        return () => {
            if (rafRef.current !== null) {
                cancelAnimationFrame(rafRef.current);
            }
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [value, duration, highlightChange]);

    const flashClass =
        flash === "increase"
            ? "text-emerald-600"
            : flash === "decrease"
            ? "text-destructive"
            : "";

    return (
        <AnimatePresence mode="wait">
            <motion.span
                key={flash ?? "stable"}
                initial={{ opacity: 0.6 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0.6 }}
                transition={{ duration: 0.25 }}
                className={cn(
                    "tabular-nums transition-colors duration-500",
                    flashClass,
                    className
                )}
            >
                {format(displayValue)}
            </motion.span>
        </AnimatePresence>
    );
}
