"use client";

import React from "react";
import Link from "next/link";
import { ExternalLink, FileCheck2, ShieldCheck } from "lucide-react";
import { useNetwork } from "@/context/NetworkProvider";
import type { NetworkConfig } from "@/lib/networks";

export const VERIFY_DOC_URL =
    "https://github.com/Suncrest-Labs/nester/blob/dev/VERIFY.md";

export interface ContractDescriptor {
    key: keyof NetworkConfig["contracts"];
    label: string;
    description: string;
    wasmHashEnvKey: string;
    defaultWasmHash: string;
}

export const CONTRACT_DESCRIPTORS: ContractDescriptor[] = [
    {
        key: "vault",
        label: "Vault",
        description: "Core SEP-41 savings & yield vault contract",
        wasmHashEnvKey: "NEXT_PUBLIC_VAULT_WASM_HASH",
        defaultWasmHash: "sha256:verified-in-VERIFY.md",
    },
    {
        key: "vaultToken",
        label: "Vault Share Token",
        description: "Fungible share token representing vault deposits",
        wasmHashEnvKey: "NEXT_PUBLIC_VAULT_TOKEN_WASM_HASH",
        defaultWasmHash: "sha256:verified-in-VERIFY.md",
    },
    {
        key: "yieldRegistry",
        label: "Yield Registry",
        description: "On-chain registry of approved yield sources",
        wasmHashEnvKey: "NEXT_PUBLIC_YIELD_REGISTRY_WASM_HASH",
        defaultWasmHash: "sha256:verified-in-VERIFY.md",
    },
    {
        key: "allocationStrategy",
        label: "Allocation Strategy",
        description: "Automated allocation and rebalance controller",
        wasmHashEnvKey: "NEXT_PUBLIC_ALLOCATION_STRATEGY_WASM_HASH",
        defaultWasmHash: "sha256:verified-in-VERIFY.md",
    },
    {
        key: "treasury",
        label: "Treasury",
        description: "Protocol fee and reserve treasury contract",
        wasmHashEnvKey: "NEXT_PUBLIC_TREASURY_WASM_HASH",
        defaultWasmHash: "sha256:verified-in-VERIFY.md",
    },
    {
        key: "accessControl",
        label: "Access Control",
        description: "Role-based governance and timelock guard",
        wasmHashEnvKey: "NEXT_PUBLIC_ACCESS_CONTROL_WASM_HASH",
        defaultWasmHash: "sha256:verified-in-VERIFY.md",
    },
];

export function buildStellarExpertContractUrl(
    networkId: "testnet" | "mainnet",
    contractId: string,
): string {
    const netSegment = networkId === "mainnet" ? "public" : "testnet";
    return `https://stellar.expert/explorer/${netSegment}/contract/${encodeURIComponent(contractId)}`;
}

export function DeployedContractsSection() {
    const { currentNetwork } = useNetwork();

    return (
        <section
            aria-labelledby="deployed-contracts-heading"
            data-testid="deployed-contracts-section"
            data-network={currentNetwork.id}
            className="mt-10 rounded-2xl border border-black/8 dark:border-white/10 bg-white/70 dark:bg-white/[0.02] p-6"
        >
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-5">
                <div className="flex items-start gap-3">
                    <div className="mt-0.5 flex h-9 w-9 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
                        <ShieldCheck className="h-5 w-5" />
                    </div>
                    <div>
                        <h2
                            id="deployed-contracts-heading"
                            className="text-base font-semibold text-black dark:text-white"
                        >
                            Contracts ({currentNetwork.name})
                        </h2>
                        <p className="text-xs text-black/50 dark:text-white/50">
                            Verify every deployed Soroban contract on Stellar Expert and compare its WASM hash against our reproducible build instructions.
                        </p>
                    </div>
                </div>

                <div className="flex items-center gap-3">
                    <Link
                        href="/security"
                        className="inline-flex items-center gap-1.5 rounded-lg border border-black/10 dark:border-white/15 px-3 py-1.5 text-xs font-medium text-black/80 dark:text-white/80 hover:bg-black/5 dark:hover:bg-white/5"
                    >
                        Security Overview
                    </Link>
                    <a
                        href={VERIFY_DOC_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        data-testid="verify-doc-link"
                        className="inline-flex items-center gap-1.5 rounded-lg bg-emerald-500/10 px-3 py-1.5 text-xs font-semibold text-emerald-700 dark:text-emerald-300 hover:bg-emerald-500/20"
                    >
                        <FileCheck2 className="h-3.5 w-3.5" />
                        VERIFY.md
                        <ExternalLink className="h-3 w-3" />
                    </a>
                </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {CONTRACT_DESCRIPTORS.map((item) => {
                    const contractId = currentNetwork.contracts[item.key];
                    const explorerHref = buildStellarExpertContractUrl(
                        currentNetwork.id,
                        contractId,
                    );
                    const wasmHash =
                        (typeof process !== "undefined" &&
                            process.env?.[item.wasmHashEnvKey]) ||
                        item.defaultWasmHash;

                    return (
                        <div
                            key={item.key}
                            data-testid={`contract-card-${item.key}`}
                            className="flex flex-col justify-between rounded-xl border border-black/8 dark:border-white/8 bg-black/[0.015] dark:bg-white/[0.02] p-4"
                        >
                            <div>
                                <div className="flex items-center justify-between gap-2">
                                    <span className="text-xs font-semibold text-black dark:text-white">
                                        {item.label}
                                    </span>
                                    <a
                                        href={explorerHref}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        data-testid={`contract-explorer-link-${item.key}`}
                                        className="inline-flex items-center gap-1 text-[11px] font-medium text-emerald-600 dark:text-emerald-400 hover:underline"
                                    >
                                        stellar.expert
                                        <ExternalLink className="h-3 w-3" />
                                    </a>
                                </div>
                                <p className="mt-1 text-[11px] text-black/50 dark:text-white/50">
                                    {item.description}
                                </p>
                            </div>

                            <div className="mt-3 pt-2.5 border-t border-black/5 dark:border-white/5 space-y-1">
                                <div className="flex items-center justify-between gap-2 text-[11px]">
                                    <span className="text-black/45 dark:text-white/45">
                                        Contract ID
                                    </span>
                                    <code
                                        data-testid={`contract-id-${item.key}`}
                                        className="font-mono text-[11px] text-black/80 dark:text-white/80 break-all"
                                    >
                                        {contractId}
                                    </code>
                                </div>
                                <div className="flex items-center justify-between gap-2 text-[11px]">
                                    <span className="text-black/45 dark:text-white/45">
                                        WASM Hash
                                    </span>
                                    <code
                                        data-testid={`contract-wasm-${item.key}`}
                                        className="font-mono text-[10px] text-black/60 dark:text-white/60"
                                    >
                                        {wasmHash}
                                    </code>
                                </div>
                            </div>
                        </div>
                    );
                })}
            </div>
        </section>
    );
}
