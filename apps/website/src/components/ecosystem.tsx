"use client"

import { Star, ArrowUpRight, TrendingUp, Blocks, ArrowRightLeft, Globe } from "lucide-react"
import Image from "next/image"

const projects = [
    {
        name: "Aquarius",
        tag: "AMM / Liquidity",
        desc: "Decentralized liquidity layer and voting protocol for the Stellar DEX.",
        avatar: "AQ",
        color: "#0EA5E9",
    },
    {
        name: "Blend Protocol",
        tag: "Lending",
        desc: "Permissionless lending pools built on Soroban smart contracts.",
        avatar: "BL",
        color: "#8B5CF6",
    },
    {
        name: "USDC on Stellar",
        tag: "Stablecoin",
        desc: "Native USD-backed stablecoin issued by Circle on the Stellar network.",
        avatar: "UC",
        color: "#2775CA",
    },
    {
        name: "MoneyGram Access",
        tag: "Fiat On/Off Ramp",
        desc: "Cash out USDC at 350,000+ MoneyGram locations worldwide.",
        avatar: "MG",
        color: "#E11D48",
    },
    {
        name: "Soroswap",
        tag: "DEX / AMM",
        desc: "Uniswap-style automated market maker natively on Soroban.",
        avatar: "SS",
        color: "#F59E0B",
    },
    {
        name: "Phoenix Protocol",
        tag: "DeFi Hub",
        desc: "Multi-hop DEX aggregator and yield optimizer on Soroban.",
        avatar: "PX",
        color: "#EF4444",
    },
    {
        name: "Vibrant",
        tag: "Savings App",
        desc: "Earn yield on stablecoins and send money globally from your phone.",
        avatar: "VB",
        color: "#10B981",
    },
    {
        name: "Lobstr",
        tag: "Wallet",
        desc: "The most popular non-custodial wallet for the Stellar network.",
        avatar: "LB",
        color: "#6366F1",
    },
    {
        name: "Freighter",
        tag: "Developer Wallet",
        desc: "Browser extension wallet for building and testing on Stellar.",
        avatar: "FR",
        color: "#EC4899",
    },
    {
        name: "Pendulum",
        tag: "Cross-chain Bridge",
        desc: "Connects Stellar stablecoins to EVM ecosystems via fiat rails.",
        avatar: "PD",
        color: "#14B8A6",
    },
    {
        name: "Ultra Stellar",
        tag: "Portfolio Tracker",
        desc: "Full-featured Stellar wallet with built-in DEX and analytics.",
        avatar: "US",
        color: "#F97316",
    },
    {
        name: "Lumenswap",
        tag: "DEX",
        desc: "Decentralized exchange and token issuance platform on Stellar.",
        avatar: "LS",
        color: "#A855F7",
    },
]

const features = [
    {
        label: "Diversified Yield Strategies",
        desc: "We dynamically route your assets across top-tier lending pools and AMMs on Stellar to safely optimize your returns.",
        icon: TrendingUp
    },
    {
        label: "Seamless App Integrations",
        desc: "Enjoy a frictionless user experience with our built-in integrations for leading wallets, trackers, and fiat on/off ramps.",
        icon: Blocks
    },
    {
        label: "Smart Liquidity Routing",
        desc: "Our architecture leverages Soroban to automatically source the deepest liquidity and best rates across the network.",
        icon: ArrowRightLeft
    },
    {
        label: "Unified Ecosystem Access",
        desc: "Tap into the complex landscape of decentralized finance, curated perfectly into one intuitive application.",
        icon: Globe
    },
]

function Card({ project }: { project: typeof projects[0] }) {
    return (
        <div className="group relative bg-white/[0.04] hover:bg-white/[0.08] border border-white/[0.08] rounded-2xl p-4 flex flex-col gap-3 transition-colors duration-200 cursor-default">
            <div className="flex items-start justify-between">
                <div
                    className="w-10 h-10 rounded-xl flex items-center justify-center text-white text-xs font-bold tracking-wide flex-shrink-0"
                    style={{ backgroundColor: project.color }}
                >
                    {project.avatar}
                </div>
                <Star className="w-3.5 h-3.5 text-white/20 group-hover:text-white/40 transition-colors" />
            </div>
            <div>
                <p className="text-white text-sm font-semibold leading-snug">{project.name}</p>
                <p className="text-white/40 text-[0.7rem] font-medium mt-0.5 uppercase tracking-wider">{project.tag}</p>
            </div>
            <p className="text-white/50 text-xs leading-relaxed flex-1">{project.desc}</p>

            <button className="flex items-center gap-1.5 self-start text-xs font-medium text-white/70 hover:text-white transition-colors py-1.5 select-none rounded-md group/btn mt-1">
                Website
                <ArrowUpRight className="w-3.5 h-3.5 group-hover/btn:-translate-y-[1px] group-hover/btn:translate-x-[1px] transition-transform" />
            </button>
        </div>
    )
}

const STEP_H = 68          // px — tall steps like Stacks
const BG = "#ffffff"
const R = 22          // px — clearly visible rounded inner corners

// widths decrease dramatically: 48% → 34% → 21% → 9%
const STEPS = ["48%", "34%", "21%", "9%"]

const col1 = [0, 3, 6, 9].map(i => projects[i])
const col2 = [1, 4, 7, 10].map(i => projects[i])
const col3 = [2, 5, 8, 11].map(i => projects[i])

const track1 = [...col1, ...col1]
const track2 = [...col2, ...col2]
const track3 = [...col3, ...col3]

export function Ecosystem() {
    return (
        <section className="relative w-full bg-[#0c0c0c] mb-10">

            {/* ── Top-left 4-step staircase ── */}
            {STEPS.map((w, i) => (
                <div
                    key={`tl-${i}`}
                    className="absolute left-0 z-20 hidden md:block"
                    style={{
                        top: i * STEP_H,
                        width: w,
                        height: STEP_H,
                        backgroundColor: BG,
                        borderBottomRightRadius: R,
                    }}
                />
            ))}
            {/* Inner concave corners — smooth gray fillet at each step junction */}
            {STEPS.map((w, i) => (
                <div
                    key={`tl-outer-${i}`}
                    className="absolute z-20 hidden md:block"
                    style={{
                        top: i * STEP_H,
                        left: w,
                        width: R,
                        height: R,
                        background: `radial-gradient(circle at 100% 100%, transparent ${R - 0.5}px, ${BG} ${R}px)`,
                    }}
                />
            ))}

            {/* ── Bottom-right 4-step staircase (mirrored) ── */}
            {STEPS.map((w, i) => (
                <div
                    key={`br-${i}`}
                    className="absolute right-0 z-20 hidden md:block"
                    style={{
                        bottom: i * STEP_H,
                        width: w,
                        height: STEP_H,
                        backgroundColor: BG,
                        borderTopLeftRadius: R,
                    }}
                />
            ))}
            {/* Inner concave corners — smooth gray fillet at each step junction */}
            {STEPS.map((w, i) => (
                <div
                    key={`br-outer-${i}`}
                    className="absolute z-20 hidden md:block"
                    style={{
                        bottom: i * STEP_H,
                        right: w,
                        width: R,
                        height: R,
                        background: `radial-gradient(circle at 0 0, transparent ${R - 0.5}px, ${BG} ${R}px)`,
                    }}
                />
            ))}

            {/* ── Inner layout (overflow-hidden lives here, not on section) ── */}
            <div className="flex flex-col lg:flex-row w-full overflow-hidden">

                {/* Left text panel */}
                <div className="lg:w-[40%] flex flex-col justify-center gap-8
                                px-8 pt-12 pb-12
                                md:px-14 md:pb-16
                                lg:pb-24 lg:px-16
                                border-b lg:border-b-0 border-white/[0.07]
                                ecosystem-text-panel">
                    <div>
                        <p className="text-white/40 text-xs tracking-[0.2em] uppercase font-medium mb-4">
                            Ecosystem
                        </p>
                        <h2 className="text-white text-[1.6rem] sm:text-[2rem] md:text-[2.4rem] lg:text-[2.75rem] font-light leading-[1.2] tracking-tight font-sans">
                            Unlock the full <span className="font-alpina italic font-medium text-[#c8c8c8]">potential</span> of the Stellar{" "}
                            ecosystem
                            <span
                                className="inline-flex items-center justify-center align-middle mx-1 rounded-full overflow-hidden shadow-sm"
                                style={{
                                    width: "clamp(36px, 4.5vw, 56px)",
                                    height: "clamp(36px, 4.5vw, 56px)",
                                    verticalAlign: "middle",
                                    backgroundColor: "#c8c8c8",
                                    transform: "rotate(15deg) translateY(-2px)"
                                }}
                            >
                                <Image src="/logos/Stellar.png" alt="Stellar" width={40} height={40} className="object-cover p-1.5" />
                            </span> 
                            and interact directly with top protocols all directly through <span className="font-alpina italic font-light">Nester.</span>
                        </h2>
                    </div>

                    <div className="flex flex-col gap-5">
                        {features.map((f) => (
                            <div key={f.label} className="flex gap-4 items-start">
                                <div className="w-5 h-5 rounded-md bg-white/10 mt-[0.15rem] flex items-center justify-center flex-shrink-0">
                                    <f.icon className="w-3 h-3 text-white/70" />
                                </div>
                                <div>
                                    <p className="font-sans text-white/70 text-xs tracking-wider uppercase font-semibold mb-1">
                                        {f.label}
                                    </p>
                                    <p className="text-white/35 text-sm leading-relaxed">{f.desc}</p>
                                </div>
                            </div>
                        ))}
                    </div>
                </div>

                {/* Right card grid */}
                <div className="flex-1 relative overflow-hidden flex flex-col px-4 md:px-12">
                    {/* Top fade */}
                    <div className="pointer-events-none absolute top-0 left-0 right-0 h-20 bg-gradient-to-b from-[#0c0c0c] to-transparent z-10" />
                    {/* Bottom fade */}
                    <div className="pointer-events-none absolute bottom-0 left-0 right-0 h-24 bg-gradient-to-t from-[#0c0c0c] to-transparent z-10" />

                    <div
                        className="flex-1 overflow-hidden flex gap-3 lg:gap-4 w-full"
                    >
                        <div className="flex-1 min-w-0 group/col relative">
                            <div className="absolute inset-x-0 flex flex-col gap-3 lg:gap-4 animate-marquee-up overflow-visible group-hover/col:[animation-play-state:paused]">
                                {track1.map((p, i) => (
                                    <Card key={`c1-${i}`} project={p} />
                                ))}
                            </div>
                        </div>
                        <div className="flex-1 min-w-0 group/col relative">
                            <div className="absolute inset-x-0 flex flex-col gap-3 lg:gap-4 animate-marquee-down overflow-visible group-hover/col:[animation-play-state:paused]" style={{ animationDelay: '-15s' }}>
                                {track2.map((p, i) => (
                                    <Card key={`c2-${i}`} project={p} />
                                ))}
                            </div>
                        </div>
                        <div className="hidden lg:block flex-1 min-w-0 group/col relative">
                            <div className="absolute inset-x-0 flex flex-col gap-3 lg:gap-4 animate-marquee-up overflow-visible group-hover/col:[animation-play-state:paused]" style={{ animationDelay: '-25s' }}>
                                {track3.map((p, i) => (
                                    <Card key={`c3-${i}`} project={p} />
                                ))}
                            </div>
                        </div>
                    </div>
                </div>

            </div>
        </section>
    )
}
