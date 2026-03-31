"use client";

import { useEffect, useState, useCallback } from "react";
import { createPortal } from "react-dom";
import { motion, AnimatePresence } from "framer-motion";
import { useOnboarding } from "@/hooks/useOnboarding";

interface TourStep {
    id: string;
    selector: string;
    title: string;
    content: string;
    position: "top" | "bottom" | "left" | "right";
}

const TOUR_STEPS: TourStep[] = [
    {
        id: "portfolio",
        selector: "[data-tour='portfolio-overview']",
        title: "Portfolio Overview",
        content: "This is your portfolio summary showing your total balance, active vaults, and yield earned.",
        position: "bottom",
    },
    {
        id: "vaults",
        selector: "[data-tour='vault-list']",
        title: "Vault List",
        content: "Browse available vaults to deposit your stablecoins and start earning automated yield.",
        position: "top",
    },
    {
        id: "deposit",
        selector: "[data-tour='deposit-cta']",
        title: "Make a Deposit",
        content: "Click here to add funds to a vault and start earning yield immediately.",
        position: "left",
    },
    {
        id: "settlements",
        selector: "[data-tour='settlements-tab']",
        title: "Settlements",
        content: "Convert your earned yield directly to your local currency through our seamless off-ramp.",
        position: "bottom",
    },
];

export function GuidedTour() {
    const { hasCompletedTour, hasConnectedWallet, completeStep } = useOnboarding();
    const [currentStepIndex, setCurrentStepIndex] = useState(0);
    const [targetRect, setTargetRect] = useState<DOMRect | null>(null);
    const [isMounted, setIsMounted] = useState(false);

    const isActive = hasConnectedWallet && !hasCompletedTour;

    const updatePosition = useCallback(() => {
        if (!isActive) return;
        
        const step = TOUR_STEPS[currentStepIndex];
        const element = document.querySelector(step.selector);
        
        if (element) {
            const rect = element.getBoundingClientRect();
            setTargetRect(rect);
            
            // Scroll into view if needed
            const isInView = (
                rect.top >= 0 &&
                rect.left >= 0 &&
                rect.bottom <= (window.innerHeight || document.documentElement.clientHeight) &&
                rect.right <= (window.innerWidth || document.documentElement.clientWidth)
            );
            
            if (!isInView) {
                element.scrollIntoView({ behavior: "smooth", block: "center" });
            }
        } else {
            // If element not found, wait and try again (might be rendering)
            setTimeout(() => {
                const retryEl = document.querySelector(step.selector);
                if (retryEl) {
                    setTargetRect(retryEl.getBoundingClientRect());
                } else {
                    // Skip this step if element doesn't exist on this page
                    handleNext();
                }
            }, 500);
        }
    }, [currentStepIndex, isActive]);

    useEffect(() => {
        setIsMounted(true);
    }, []);

    useEffect(() => {
        if (isActive) {
            updatePosition();
            window.addEventListener("resize", updatePosition);
            window.addEventListener("scroll", updatePosition, { passive: true });
            
            return () => {
                window.removeEventListener("resize", updatePosition);
                window.removeEventListener("scroll", updatePosition);
            };
        }
    }, [isActive, currentStepIndex, updatePosition]);

    const handleNext = () => {
        if (currentStepIndex < TOUR_STEPS.length - 1) {
            setCurrentStepIndex(prev => prev + 1);
        } else {
            completeStep("hasCompletedTour");
        }
    };

    const handleSkip = () => {
        completeStep("hasCompletedTour");
    };

    if (!isMounted || !isActive || !targetRect) return null;

    const step = TOUR_STEPS[currentStepIndex];
    
    // Calculate tooltip position based on target rect and preferred position
    let top = 0;
    let left = 0;
    
    // Some padding from the target element
    const offset = 16;
    
    if (step.position === "bottom") {
        top = targetRect.bottom + offset;
        left = targetRect.left + (targetRect.width / 2);
    } else if (step.position === "top") {
        top = targetRect.top - offset;
        left = targetRect.left + (targetRect.width / 2);
    } else if (step.position === "left") {
        top = targetRect.top + (targetRect.height / 2);
        left = targetRect.left - offset;
    } else if (step.position === "right") {
        top = targetRect.top + (targetRect.height / 2);
        left = targetRect.right + offset;
    }

    return createPortal(
        <div className="fixed inset-0 z-[100] pointer-events-none">
            {/* Highlight Overlay */}
            <div 
                className="absolute inset-0 bg-background/50 transition-all duration-300"
                style={{
                    clipPath: `polygon(
                        0% 0%, 0% 100%, 100% 100%, 100% 0%, 0% 0%,
                        ${targetRect.left - 8}px ${targetRect.top - 8}px,
                        ${targetRect.right + 8}px ${targetRect.top - 8}px,
                        ${targetRect.right + 8}px ${targetRect.bottom + 8}px,
                        ${targetRect.left - 8}px ${targetRect.bottom + 8}px,
                        ${targetRect.left - 8}px ${targetRect.top - 8}px
                    )`
                }}
            />
            
            {/* Target Highlight Border */}
            <div 
                className="absolute rounded-xl border-2 border-foreground transition-all duration-300 pointer-events-none"
                style={{
                    top: targetRect.top - 8,
                    left: targetRect.left - 8,
                    width: targetRect.width + 16,
                    height: targetRect.height + 16,
                }}
            />

            {/* Tooltip */}
            <AnimatePresence mode="wait">
                <motion.div
                    key={step.id}
                    initial={{ opacity: 0, scale: 0.9, y: step.position === 'top' ? 10 : step.position === 'bottom' ? -10 : 0 }}
                    animate={{ opacity: 1, scale: 1, y: 0 }}
                    exit={{ opacity: 0, scale: 0.9 }}
                    transition={{ duration: 0.2 }}
                    className="absolute pointer-events-auto w-64 md:w-72 rounded-2xl bg-foreground text-background shadow-2xl p-5"
                    style={{
                        top: top,
                        left: left,
                        transform: `translate(${step.position === 'left' ? '-100%' : step.position === 'right' ? '0' : '-50%'}, ${step.position === 'top' ? '-100%' : step.position === 'bottom' ? '0' : '-50%'})`
                    }}
                >
                    <div className="flex justify-between items-start mb-3">
                        <h4 className="font-medium text-sm">{step.title}</h4>
                        <span className="text-[10px] font-medium px-2 py-0.5 rounded-full bg-background/20 text-background">
                            {currentStepIndex + 1} / {TOUR_STEPS.length}
                        </span>
                    </div>
                    <p className="text-xs text-background/80 mb-5 leading-relaxed">
                        {step.content}
                    </p>
                    <div className="flex justify-between items-center">
                        <button
                            onClick={handleSkip}
                            className="text-xs font-medium text-background/60 hover:text-background transition-colors"
                        >
                            Skip tour
                        </button>
                        <button
                            onClick={handleNext}
                            className="bg-background text-foreground text-xs font-medium px-4 py-2.5 rounded-xl hover:bg-background/90 transition-transform hover:scale-[1.02] active:scale-[0.98]"
                        >
                            {currentStepIndex === TOUR_STEPS.length - 1 ? "Finish" : "Next"}
                        </button>
                    </div>
                </motion.div>
            </AnimatePresence>
        </div>,
        document.body
    );
}
