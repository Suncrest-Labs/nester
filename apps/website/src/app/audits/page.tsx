import { Container } from "@/components/container";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Security Audit | Nester",
  description: "Security audit scope, threat model, and published findings for Nester smart contracts.",
};

const scope = [
  "vault",
  "vault_token",
  "nester",
  "treasury",
  "allocation_strategy",
  "access_control",
  "timelock",
];

const threats = [
  "Privilege escalation through admin or governance paths",
  "Reentrancy in deposit, withdraw, and rebalance flows",
  "Integer overflow or precision loss in accounting code",
  "Fee manipulation and rounding drift in yield accounting",
  "Unsafe cross-contract calls or stale state transitions",
];

export default function AuditsPage() {
  return (
    <main className="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(15,23,42,0.9),_rgba(2,6,23,1)_60%)] text-white">
      <Container className="py-20 md:py-28">
        <div className="max-w-4xl">
          <p className="mb-4 text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">
            Security Audit
          </p>
          <h1 className="max-w-3xl text-4xl font-semibold tracking-tight md:text-6xl">
            Audit package, threat model, and published findings.
          </h1>
          <p className="mt-6 max-w-2xl text-base leading-7 text-slate-300 md:text-lg">
            Nester&apos;s smart contracts move user funds on Stellar Mainnet, so the audit
            process focuses on clear scope, narrow attack surfaces, and a public record of
            findings once the review is complete.
          </p>
        </div>

        <div className="mt-14 grid gap-6 lg:grid-cols-2">
          <section className="rounded-3xl border border-white/10 bg-white/5 p-6 backdrop-blur-sm">
            <h2 className="text-lg font-medium text-white">In Scope</h2>
            <ul className="mt-4 space-y-3 text-sm leading-6 text-slate-300">
              {scope.map((item) => (
                <li key={item} className="rounded-2xl border border-white/5 bg-black/20 px-4 py-3">
                  {item}
                </li>
              ))}
            </ul>
          </section>

          <section className="rounded-3xl border border-white/10 bg-white/5 p-6 backdrop-blur-sm">
            <h2 className="text-lg font-medium text-white">Threat Model</h2>
            <ul className="mt-4 space-y-3 text-sm leading-6 text-slate-300">
              {threats.map((item) => (
                <li key={item} className="rounded-2xl border border-white/5 bg-black/20 px-4 py-3">
                  {item}
                </li>
              ))}
            </ul>
          </section>
        </div>

        <section className="mt-6 rounded-3xl border border-emerald-400/20 bg-emerald-400/10 p-6">
          <h2 className="text-lg font-medium text-white">Current Status</h2>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-200">
            The audit package is prepared for external review. Findings will be triaged by
            severity, critical and high issues will block launch, and the published report will
            be linked here once available.
          </p>
        </section>
      </Container>
    </main>
  );
}
