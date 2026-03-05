"use client"

import { motion } from "framer-motion"

const stats = [
    { label: "Total Liquidity", value: "$12.4B" },
    { label: "Total Borrows", value: "$4.8B" },
    { label: "Rewards Paid", value: "$182M" },
    { label: "Active Nodes", value: "8,240" },
]

export function StatsBar() {
    return (
        <section className="bg-muted/30 border-y border-border relative z-10 w-full">
            <div className="mx-auto w-full max-w-[1280px] px-6 lg:px-8">
                <div className="grid grid-cols-2 md:grid-cols-4 divide-x divide-border">
                    {stats.map((stat, i) => (
                        <motion.div
                            key={stat.label}
                            initial={{ opacity: 0, y: 10 }}
                            whileInView={{ opacity: 1, y: 0 }}
                            viewport={{ once: true }}
                            transition={{ delay: i * 0.1, duration: 0.5 }}
                            className="px-6 py-8 flex flex-col items-center justify-center text-center group cursor-default hover:bg-white/50 transition-colors first:pl-0 last:pr-0"
                        >
                            <span className="text-muted-foreground text-xs lg:text-sm font-bold tracking-widest uppercase mb-1">{stat.label}</span>
                            <span className="text-2xl lg:text-3xl font-heading font-bold text-foreground group-hover:text-primary transition-colors">{stat.value}</span>
                        </motion.div>
                    ))}
                </div>
            </div>
        </section>
    )
}
