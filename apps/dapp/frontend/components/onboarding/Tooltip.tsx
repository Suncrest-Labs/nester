"use client";

import { motion, AnimatePresence } from "framer-motion";
import { ReactNode } from "react";

interface TooltipProps {
    isOpen: boolean;
    title: string;
    content: string;
    step: number;
    totalSteps: number;
    onNext: () => void;
    onSkip: () => void;
    position?: "top" | "bottom" | "left" | "right";
    targetElement?: HTMLElement | null;
}

export function Tooltip({
    isOpen,
    title,
    content,
    step,
    totalSteps,
    onNext,
    onSkip,
    position = "bottom",
}: TooltipProps) {
    if (!isOpen) return null;

    // We'll let the parent position this relative to the target element using fixed/absolute
    return (
        <AnimatePresence>
            {isOpen && (
                <motion.div
                    initial={{ opacity: 0, y: position === "bottom" ? -10 : 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    exit={{ opacity: 0, y: position === "bottom" ? -10 : 10 }}
                    className={`absolute z-[100] w-64 md:w-72 rounded-2xl bg-foreground text-background shadow-xl p-4
                        ${position === "bottom" ? "top-full mt-4 left-1/2 -translate-x-1/2" : ""}
                        ${position === "top" ? "bottom-full mb-4 left-1/2 -translate-x-1/2" : ""}
                        ${position === "left" ? "right-full mr-4 top-1/2 -translate-y-1/2" : ""}
                        ${position === "right" ? "left-full ml-4 top-1/2 -translate-y-1/2" : ""}
                    `}
                >
                    {/* Arrow */}
                    <div
                        className={`absolute w-3 h-3 bg-foreground transform rotate-45
                            ${position === "bottom" ? "-top-1.5 left-1/2 -translate-x-1/2" : ""}
                            ${position === "top" ? "-bottom-1.5 left-1/2 -translate-x-1/2" : ""}
                            ${position === "left" ? "-right-1.5 top-1/2 -translate-y-1/2" : ""}
                            ${position === "right" ? "-left-1.5 top-1/2 -translate-y-1/2" : ""}
                        `}
                    />
                    
                    <div className="relative z-10">
                        <div className="flex justify-between items-start mb-2">
                            <h4 className="font-medium text-sm">{title}</h4>
                            <span className="text-xs text-background/60">
                                {step} of {totalSteps}
                            </span>
                        </div>
                        <p className="text-xs text-background/80 mb-4 leading-relaxed">
                            {content}
                        </p>
                        <div className="flex justify-between items-center">
                            <button
                                onClick={onSkip}
                                className="text-xs text-background/60 hover:text-background transition-colors"
                            >
                                Skip tour
                            </button>
                            <button
                                onClick={onNext}
                                className="bg-background text-foreground text-xs font-medium px-4 py-2 rounded-lg hover:bg-background/90 transition-colors"
                            >
                                {step === totalSteps ? "Finish" : "Next"}
                            </button>
                        </div>
                    </div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
