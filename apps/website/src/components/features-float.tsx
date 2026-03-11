"use client"

import { useRef } from "react"
import { motion, useScroll, useTransform, MotionValue } from "framer-motion"
import {
    Send, Sparkles, TrendingUp,
    ArrowRightLeft, Globe, Building2,
} from "lucide-react"

const leftFeatures = [
    { label: "Cross-border Payments", icon: Send,            color: "bg-blue-500",    rotate: 10  },
    { label: "Smart AI DeFi Agents",  icon: Sparkles,        color: "bg-violet-500",  rotate: 6   },
    { label: "Yield Aggregator",      icon: TrendingUp,      color: "bg-emerald-500", rotate: -12 },
]

const rightFeatures = [
    { label: "DEX / AMMs",            icon: ArrowRightLeft,  color: "bg-pink-500",    rotate: -8  },
    { label: "Emerging Markets",      icon: Globe,           color: "bg-cyan-500",    rotate: -11 },
    { label: "Real World Assets",     icon: Building2,       color: "bg-amber-500",   rotate: 6   },
]

function Pill({ label, icon: Icon, color, rotate = 0 }: {
    label: string
    icon: React.ElementType
    color: string
    rotate?: number
}) {
    return (
        <div
            className="p-[3px] rounded-full border border-black/[0.07] shadow-[0_4px_18px_rgba(0,0,0,0.13)] bg-black/[0.04] w-fit"
            style={{ transform: `rotate(${rotate}deg)` }}
        >
            <div className="flex items-center gap-3 bg-white rounded-full pl-1.5 pr-5 py-1.5">
                <span className={`${color} rounded-full p-2 flex items-center justify-center`}>
                    <Icon className="w-4 h-4 text-white" strokeWidth={2} />
                </span>
                <span className="text-[0.9rem] font-medium text-gray-800 whitespace-nowrap">{label}</span>
            </div>
        </div>
    )
}

function Word({ children, progress, range }: {
    children: string
    progress: MotionValue<number>
    range: [number, number]
}) {
    const color = useTransform(progress, range, ["#c0c0c0", "#111111"])
    return (
        <motion.span style={{ color }} className="inline-block mr-[0.25em]">
            {children}
        </motion.span>
    )
}

const headline = "We help anyone, anywhere move and grow money — on-chain."
const words = headline.split(" ")

export function FeaturesFloat() {
    const ref = useRef<HTMLElement>(null)

    const { scrollYProgress } = useScroll({
        target: ref,
        offset: ["start 0.8", "end 0.3"],
    })

    const leftY  = useTransform(scrollYProgress, [0, 1], [0, -80])
    const rightY = useTransform(scrollYProgress, [0, 1], [0,  80])

    return (
        <section ref={ref} className="relative w-full px-4 pt-16 pb-20 md:pt-20 md:pb-28 overflow-hidden">
            <div className="max-w-7xl mx-auto flex items-center justify-between gap-8">

                {/* Left pills */}
                <motion.div
                    style={{ y: leftY }}
                    className="hidden lg:flex flex-col gap-14 flex-shrink-0 items-start"
                >
                    {leftFeatures.map((f) => (
                        <Pill key={f.label} {...f} />
                    ))}
                </motion.div>

                {/* Center text */}
                <div className="flex-1 text-center flex flex-col items-center gap-4">
                    <div className="flex items-center gap-3 text-gray-400 text-sm tracking-widest">
                        <span className="h-px w-10 bg-gray-400/60" />
                        <span className="italic font-light">What we build</span>
                        <span className="h-px w-10 bg-gray-400/60" />
                    </div>

                    <h2 className="text-[1.75rem] sm:text-[2.25rem] md:text-[2.75rem] lg:text-[3.25rem] font-light leading-[1.15] tracking-tight max-w-xl">
                        {words.map((word, i) => {
                            const start = i / words.length
                            const end = (i + 1) / words.length
                            return (
                                <Word key={i} progress={scrollYProgress} range={[start, end]}>
                                    {word}
                                </Word>
                            )
                        })}
                    </h2>

                    {/* Mobile pills */}
                    <div className="flex lg:hidden flex-wrap justify-center gap-2 mt-4">
                        {[...leftFeatures, ...rightFeatures].map((f) => (
                            <Pill key={f.label} {...f} rotate={0} />
                        ))}
                    </div>
                </div>

                {/* Right pills */}
                <motion.div
                    style={{ y: rightY }}
                    className="hidden lg:flex flex-col gap-14 flex-shrink-0 items-end"
                >
                    {rightFeatures.map((f) => (
                        <Pill key={f.label} {...f} />
                    ))}
                </motion.div>

            </div>
        </section>
    )
}
