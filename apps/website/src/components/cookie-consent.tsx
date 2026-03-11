"use client"

import * as React from "react"
import { motion, AnimatePresence } from "framer-motion"
import { Button } from "@/components/ui/button"

export function CookieConsent() {
    const [isVisible, setIsVisible] = React.useState(false)

    React.useEffect(() => {
        // Check if user has already consented
        const consented = localStorage.getItem("nester-cookie-consent")
        if (!consented) {
            // Show after a small delay
            const timer = setTimeout(() => setIsVisible(true), 1000)
            return () => clearTimeout(timer)
        }
    }, [])

    const handleAccept = () => {
        localStorage.setItem("nester-cookie-consent", "true")
        setIsVisible(false)
    }

    const handleDecline = () => {
        localStorage.setItem("nester-cookie-consent", "false")
        setIsVisible(false)
    }

    return (
        <AnimatePresence>
            {isVisible && (
                <motion.div
                    initial={{ opacity: 0, y: 20, scale: 0.95 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: 20, scale: 0.95 }}
                    transition={{ duration: 0.3 }}
                    className="fixed bottom-4 left-4 right-4 sm:left-auto sm:right-6 sm:bottom-6 z-[100] w-auto sm:w-full sm:max-w-[420px] p-5 sm:p-6 bg-white rounded-[20px] sm:rounded-[24px] shadow-2xl border border-border/10"
                >
                    <div className="flex flex-col gap-6">
                        <p className="text-[15px] text-[#111827] leading-relaxed font-sans">
                            We use cookies to enhance your user experience, provide personalised content and analyse traffic.{" "}
                            <a href="#" className="underline decoration-1 underline-offset-2 hover:text-nester-blue transition-colors">Cookie Policy</a>
                        </p>

                        <div className="flex items-center gap-3">
                            <Button
                                onClick={handleAccept}
                                className="bg-[#111827] hover:bg-black text-white rounded-full px-6 h-11 font-medium text-sm transition-transform active:scale-95 shadow-none"
                            >
                                Accept All
                            </Button>
                            <Button
                                onClick={handleDecline}
                                variant="outline"
                                className="bg-white border-[#E5E7EB] hover:bg-gray-50 text-[#111827] rounded-full px-6 h-11 font-medium text-sm transition-transform active:scale-95"
                            >
                                Deny All
                            </Button>
                        </div>
                    </div>
                </motion.div>
            )}
        </AnimatePresence>
    )
}
