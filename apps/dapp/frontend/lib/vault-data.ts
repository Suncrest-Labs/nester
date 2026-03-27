"use client";

import {
    Layers,
    ShieldCheck,
    Vault,
    Zap,
    type LucideIcon,
} from "lucide-react";

export interface VaultDefinition {
    id: string;
    name: string;
    apy: number;
    apyLabel: string;
    description: string;
    risk: string;
    icon: LucideIcon;
    color: string;
    strategies: string[];
    lockDays: number;
    earlyWithdrawalPenaltyPct: number;
    performanceFeePct: number;
    managementFeePct: number;
    asset: "USDC";
}

export const vaultDefinitions: VaultDefinition[] = [
    {
        id: "conservative",
        name: "Conservative",
        apy: 0.07,
        apyLabel: "6-8%",
        description:
            "Focus on safety and stability using battle-tested lending protocols like Blend and Aave.",
        risk: "Low",
        icon: ShieldCheck,
        color: "emerald",
        strategies: ["Blend Lending", "Aave stable pools"],
        lockDays: 30,
        earlyWithdrawalPenaltyPct: 0.1,
        performanceFeePct: 10,
        managementFeePct: 0.5,
        asset: "USDC",
    },
    {
        id: "balanced",
        name: "Balanced",
        apy: 0.095,
        apyLabel: "8-11%",
        description:
            "Optimized mix of stable lending and high-liquidity automated market maker pools.",
        risk: "Medium",
        icon: Vault,
        color: "blue",
        strategies: ["Lending + LP", "Kamino integration"],
        lockDays: 45,
        earlyWithdrawalPenaltyPct: 0.1,
        performanceFeePct: 10,
        managementFeePct: 0.5,
        asset: "USDC",
    },
    {
        id: "growth",
        name: "Growth",
        apy: 0.13,
        apyLabel: "11-15%",
        description:
            "Dynamic strategies focusing on higher-yielding opportunities with automated risk management.",
        risk: "Moderate High",
        icon: Zap,
        color: "orange",
        strategies: ["Leveraged yield", "Volatile LP"],
        lockDays: 60,
        earlyWithdrawalPenaltyPct: 0.1,
        performanceFeePct: 10,
        managementFeePct: 0.5,
        asset: "USDC",
    },
    {
        id: "defi500",
        name: "DeFi500 Index",
        apy: 0.108,
        apyLabel: "Variable",
        description:
            "A diversified index fund of top DeFi protocols, rebalanced monthly for broad exposure.",
        risk: "Dynamic",
        icon: Layers,
        color: "purple",
        strategies: ["Index position", "Multi-protocol"],
        lockDays: 90,
        earlyWithdrawalPenaltyPct: 0.1,
        performanceFeePct: 10,
        managementFeePct: 0.5,
        asset: "USDC",
    },
];

export function getVaultById(id: string): VaultDefinition | undefined {
    return vaultDefinitions.find((vault) => vault.id === id);
}

