"use client";

import { useWallet } from "@/components/wallet-provider";
import { Navbar } from "@/components/navbar";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { motion } from "framer-motion";
import { 
    Vault, 
    TrendingUp, 
    ShieldCheck, 
    Zap, 
    Layers,
    ArrowUpRight,
    ArrowDown
} from "lucide-react";

const vaultTypes = [
    {
        id: "conservative",
        name: "Conservative",
        apy: "6-8%",
        description: "Focus on safety and stability using battle-tested lending protocols like Blend and Aave.",
        risk: "Low",
        icon: ShieldCheck,
        color: "emerald",
        strategies: ["Blend Lending", "Aave stable pools"]
    },
    {
        id: "balanced",
        name: "Balanced",
        apy: "8-11%",
        description: "Optimized mix of stable lending and high-liquidity automated market maker (AMM) pools.",
        risk: "Medium",
        icon: Vault,
        color: "blue",
        strategies: ["Lending + LP", "Kamino integration"]
    },
    {
        id: "growth",
        name: "Growth",
        apy: "11-15%",
        description: "Dynamic strategies focusing on higher-yielding opportunities with automated risk management.",
        risk: "Moderate High",
        icon: Zap,
        color: "orange",
        strategies: ["Leveraged yield", "Volatile LP"]
    },
    {
        id: "defi500",
        name: "DeFi500 Index",
        apy: "Variable",
        description: "A diversified index fund of top 500 DeFi protocols, rebalanced monthly for maximum exposure.",
        risk: "Dynamic",
        icon: Layers,
        color: "purple",
        strategies: ["Index position", "Multi-protocol"]
    }
];

export default function VaultsPage() {
    const { isConnected } = useWallet();
    const router = useRouter();

    useEffect(() => {
        if (!isConnected) {
            router.push("/");
        }
    }, [isConnected, router]);

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-background">
            <Navbar />

            <main className="mx-auto max-w-384 px-4 md:px-8 lg:px-12 xl:px-16 pt-28 pb-16">
                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5 }}
                    className="mb-10"
                >
                    <div className="flex items-center gap-2 text-primary mb-2">
                        <Vault className="h-4 w-4" />
                        <span className="text-xs font-mono font-medium uppercase tracking-wider">Vaults Engine</span>
                    </div>
                    <h1 className="font-heading text-3xl font-light text-foreground sm:text-4xl">
                        Optimize your Yield
                    </h1>
                    <p className="mt-2 max-w-2xl text-muted-foreground">
                        Select a vault strategy that matches your risk profile. Our automated engine rebalances your position across Stellar and DeFi protocols 24/7.
                    </p>
                </motion.div>

                {/* Vault Grid */}
                <div className="grid gap-6 sm:grid-cols-2">
                    {vaultTypes.map((vault, i) => (
                        <motion.div
                            key={vault.id}
                            initial={{ opacity: 0, y: 20 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{
                                duration: 0.5,
                                delay: i * 0.1,
                            }}
                            className="group relative overflow-hidden rounded-3xl border border-border bg-white p-8 transition-all hover:border-black/15 hover:shadow-xl"
                        >
                            <div className="flex flex-col h-full">
                                <div className="mb-6 flex items-start justify-between">
                                    <div className={`p-3 rounded-2xl bg-secondary text-foreground/70`}>
                                        <vault.icon className="h-6 w-6" />
                                    </div>
                                    <div className="text-right">
                                        <p className="text-sm font-medium text-muted-foreground uppercase tracking-tight">target apy</p>
                                        <p className="text-3xl font-heading font-light text-emerald-600">{vault.apy}</p>
                                    </div>
                                </div>

                                <div className="mb-8">
                                    <h3 className="text-xl font-heading font-light text-foreground mb-2">{vault.name}</h3>
                                    <p className="text-sm text-muted-foreground leading-relaxed h-12 overflow-hidden">
                                        {vault.description}
                                    </p>
                                </div>

                                <div className="mt-auto pt-6 border-t border-border flex flex-wrap gap-2 mb-6">
                                    {vault.strategies.map(strat => (
                                        <span key={strat} className="px-2.5 py-1 rounded-full bg-secondary text-[10px] font-medium text-foreground/60 uppercase">
                                            {strat}
                                        </span>
                                    ))}
                                </div>

                                <div className="flex items-center justify-between mt-2">
                                    <div className="flex items-center gap-1.5">
                                        <div className={`h-1.5 w-1.5 rounded-full ${
                                            vault.risk === "Low" ? "bg-emerald-500" : 
                                            vault.risk === "Medium" ? "bg-blue-500" : 
                                            vault.risk === "Moderate High" ? "bg-orange-500" : "bg-purple-500"
                                        }`} />
                                        <span className="text-xs text-muted-foreground font-medium">{vault.risk} Risk</span>
                                    </div>
                                    <button
                                        onClick={() => {
                                            // Placeholder until real deposit flow is wired.
                                        }}
                                        className="flex items-center gap-1.5 text-sm font-medium text-foreground hover:gap-2 transition-all"
                                    >
                                        Deposit <ArrowUpRight className="h-4 w-4" />
                                    </button>
                                </div>
                            </div>
                        </motion.div>
                    ))}
                </div>

                {/* Info Section */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5, delay: 0.6 }}
                    className="mt-12 rounded-3xl bg-secondary/30 border border-border p-8"
                >
                    <div className="grid gap-8 lg:grid-cols-3">
                        <div className="flex flex-col gap-3">
                            <div className="h-10 w-10 rounded-xl bg-white flex items-center justify-center border border-border">
                                <TrendingUp className="h-5 w-5 text-emerald-600" />
                            </div>
                            <h4 className="font-heading font-medium text-foreground">Auto-Rebalancing</h4>
                            <p className="text-xs text-muted-foreground leading-relaxed">
                                Our contracts monitor APY fluctuations every block and re-allocate liquidity to the highest yielding sources.
                            </p>
                        </div>
                        <div className="flex flex-col gap-3">
                            <div className="h-10 w-10 rounded-xl bg-white flex items-center justify-center border border-border">
                                <ShieldCheck className="h-5 w-5 text-blue-600" />
                            </div>
                            <h4 className="font-heading font-medium text-foreground">Risk Guarded</h4>
                            <p className="text-xs text-muted-foreground leading-relaxed">
                                We prioritize safety by only integrating with protocols that have undergone multiple audits and have at least $50M TVL.
                            </p>
                        </div>
                        <div className="flex flex-col gap-3">
                            <div className="h-10 w-10 rounded-xl bg-white flex items-center justify-center border border-border">
                                <ArrowDown className="h-5 w-5 text-purple-600" />
                            </div>
                            <h4 className="font-heading font-medium text-foreground">Flexible Liquidity</h4>
                            <p className="text-xs text-muted-foreground leading-relaxed">
                                Receive <span className="font-mono text-primary">nVault</span> tokens representing your share. While liquidity is instant, some vaults may have time-locked multipliers with early exit fees.
                            </p>
                        </div>
                    </div>
                </motion.div>
            </main>
        </div>
    );
}
