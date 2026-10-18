"use client";

import React from "react";
import { AppShell } from "@/components/app-shell";
import { DeployedContractsSection, VERIFY_DOC_URL } from "@/components/network/DeployedContractsSection";
import { ExternalLink, Shield } from "lucide-react";

export default function SecurityPage() {
    return (
        <AppShell>
            <div className="mx-auto max-w-5xl py-6">
                <div className="flex items-start gap-3 mb-6">
                    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
                        <Shield className="h-5 w-5" />
                    </div>
                    <div>
                        <h1 className="text-2xl font-bold text-black dark:text-white">
                            Security &amp; On-Chain Verification
                        </h1>
                        <p className="mt-1 text-sm text-black/60 dark:text-white/60">
                            Inspect Nester&apos;s deployed Soroban smart contracts, explorer links, and reproducible WASM build verification guide.
                        </p>
                    </div>
                </div>

                <div className="rounded-2xl border border-black/8 dark:border-white/10 bg-white/70 dark:bg-white/[0.02] p-6 mb-6">
                    <h2 className="text-sm font-semibold text-black dark:text-white">
                        Independent Bytecode Verification
                    </h2>
                    <p className="mt-1.5 text-xs text-black/60 dark:text-white/60 leading-relaxed">
                        Every production contract is built reproducibly from tagged source commits and attested with GitHub Artifact Attestations. Follow{" "}
                        <a
                            href={VERIFY_DOC_URL}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 font-semibold text-emerald-600 dark:text-emerald-400 hover:underline"
                        >
                            VERIFY.md
                            <ExternalLink className="h-3 w-3" />
                        </a>{" "}
                        to verify the deployed WASM sha256 hash against the on-chain instance on Stellar.
                    </p>
                </div>

                <DeployedContractsSection />
            </div>
        </AppShell>
    );
}
