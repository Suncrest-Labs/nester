import { Container } from "@/components/container";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Security & Audit | Nester",
  description:
    "Smart contract audit status and scope, threat model, and Nester's responsible-disclosure / bug-bounty process.",
};

const REPO_URL = "https://github.com/Suncrest-Labs/nester";
const THREAT_MODEL_URL = `${REPO_URL}/blob/main/AUDIT_THREAT_MODEL.md`;
const SECURITY_POLICY_URL = `${REPO_URL}/blob/main/SECURITY.md`;
const REPORT_VULN_URL = `${REPO_URL}/security/advisories/new`;
const SECURITY_EMAIL = "security@nester.dev";

// Contracts submitted for external review. Keep in sync with the audit package
// and AUDIT_THREAT_MODEL.md.
const contractsInScope = [
  "vault",
  "vault_token",
  "allocation_strategy",
  "yield_registry",
  "nester",
  "treasury",
  "timelock",
];

const threats = [
  "Privilege escalation through admin or governance paths",
  "Reentrancy in deposit, withdraw, and rebalance flows",
  "Integer overflow or precision loss in accounting code",
  "Fee manipulation and rounding drift in yield accounting",
  "Unsafe cross-contract calls or stale state transitions",
];

export default function SecurityPage() {
  return (
    <main className="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(15,23,42,0.9),_rgba(2,6,23,1)_60%)] text-white">
      <Container className="py-20 md:py-28">
        <div className="max-w-4xl">
          <p className="mb-4 text-xs font-semibold uppercase tracking-[0.28em] text-slate-400">
            Security &amp; Audit
          </p>
          <h1 className="max-w-3xl text-4xl font-semibold tracking-tight md:text-6xl">
            Audit status, scope, and responsible disclosure.
          </h1>
          <p className="mt-6 max-w-2xl text-base leading-7 text-slate-300 md:text-lg">
            Nester&apos;s smart contracts move user funds on Stellar Mainnet, so we keep a
            public record of the audit&apos;s status, the contracts under review, and a clear
            path for security researchers to report issues.
          </p>
        </div>

        {/* Smart Contract Audit — current status (placeholder until the audit completes) */}
        <section
          id="smart-contract-audit"
          className="mt-14 rounded-3xl border border-amber-400/25 bg-amber-400/10 p-6 md:p-8"
        >
          <div className="flex flex-wrap items-center gap-3">
            <h2 className="text-lg font-medium text-white">Smart Contract Audit</h2>
            <span className="inline-flex items-center gap-2 rounded-full border border-amber-300/40 bg-amber-300/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-amber-200">
              <span className="h-1.5 w-1.5 rounded-full bg-amber-300" aria-hidden="true" />
              Pending
            </span>
          </div>
          <p className="mt-4 max-w-3xl text-sm leading-6 text-slate-200">
            <strong className="font-semibold text-white">Pending</strong> — an external smart
            contract audit is being scheduled with{" "}
            <span className="text-amber-200">[auditor name when confirmed]</span>. Findings will
            be triaged by severity; critical and high issues block launch.
          </p>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-300">
            {/*
              UPDATE PATH (when the audit is complete): replace this placeholder with the
              audit firm's name, the completion date, and a link to the published report.
            */}
            When the review is complete, this section will be updated with the audit firm&apos;s
            name, the completion date, and a link to the published report.
          </p>
        </section>

        <div className="mt-6 grid gap-6 lg:grid-cols-2">
          {/* Contracts in scope */}
          <section className="rounded-3xl border border-white/10 bg-white/5 p-6 backdrop-blur-sm">
            <h2 className="text-lg font-medium text-white">Contracts in Scope</h2>
            <ul className="mt-4 space-y-3 text-sm leading-6 text-slate-300">
              {contractsInScope.map((item) => (
                <li
                  key={item}
                  className="rounded-2xl border border-white/5 bg-black/20 px-4 py-3 font-mono"
                >
                  {item}
                </li>
              ))}
            </ul>
          </section>

          {/* Threat Model */}
          <section className="rounded-3xl border border-white/10 bg-white/5 p-6 backdrop-blur-sm">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-lg font-medium text-white">Threat Model</h2>
              <a
                href={THREAT_MODEL_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1.5 rounded-full border border-white/15 px-3 py-1 text-xs font-medium text-slate-200 transition-colors hover:border-white/30 hover:text-white"
              >
                Read the threat model
                <span aria-hidden="true">↗</span>
              </a>
            </div>
            <p className="mt-4 text-sm leading-6 text-slate-300">
              The full threat model documents trust assumptions, attacker capabilities, and the
              mitigations for each contract. Key areas under review:
            </p>
            <ul className="mt-4 space-y-3 text-sm leading-6 text-slate-300">
              {threats.map((item) => (
                <li key={item} className="rounded-2xl border border-white/5 bg-black/20 px-4 py-3">
                  {item}
                </li>
              ))}
            </ul>
          </section>
        </div>

        {/* Bug Bounty / Responsible Disclosure */}
        <section
          id="bug-bounty"
          className="mt-6 rounded-3xl border border-emerald-400/20 bg-emerald-400/10 p-6 md:p-8"
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-lg font-medium text-white">
              Funded Bug Bounty &amp; Responsible Disclosure
            </h2>
            <span className="inline-flex items-center gap-2 rounded-full border border-emerald-300/40 bg-emerald-300/10 px-3 py-1 text-xs font-semibold uppercase tracking-wide text-emerald-200">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-300" aria-hidden="true" />
              Active — $50,000 USDC Pool Earmarked
            </span>
          </div>
          <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-200">
            We run a funded bug bounty covering all launch Soroban smart contracts and the API
            money-movement path (`apps/api/internal/signing`, `apps/api/cmd/signer`, and deposit/withdrawal handlers).
            Payouts scale with funds at risk (capped at 10% of affected TVL up to the tier ceiling)
            and are paid in USDC from an earmarked reserve held in the Nester Treasury Multisig.
            Please report vulnerabilities privately — do{" "}
            <strong className="font-semibold text-white">not</strong> open a public GitHub issue.
          </p>

          <div className="mt-6 overflow-x-auto rounded-2xl border border-white/10 bg-black/30">
            <table className="w-full text-left text-sm text-slate-200">
              <thead className="border-b border-white/10 text-xs uppercase tracking-wider text-slate-400">
                <tr>
                  <th className="px-4 py-3">Severity</th>
                  <th className="px-4 py-3">Scope &amp; Impact</th>
                  <th className="px-4 py-3">Reward (USDC)</th>
                  <th className="px-4 py-3">Ack / Triage SLA</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/10">
                <tr>
                  <td className="px-4 py-3 font-semibold text-rose-300">Critical</td>
                  <td className="px-4 py-3">
                    Direct loss of user funds, permanent freezing of funds, or unauthorized signing key extraction
                  </td>
                  <td className="px-4 py-3 font-mono text-emerald-300">
                    Up to $25,000 (10% of funds at risk, min $5,000)
                  </td>
                  <td className="px-4 py-3 font-mono">&lt; 4h ack / 24h triage</td>
                </tr>
                <tr>
                  <td className="px-4 py-3 font-semibold text-amber-300">High</td>
                  <td className="px-4 py-3">
                    Theft of unclaimed yield, share-price manipulation, or circuit-breaker / policy bypass
                  </td>
                  <td className="px-4 py-3 font-mono text-emerald-300">$2,000 – $10,000</td>
                  <td className="px-4 py-3 font-mono">&lt; 12h ack / 48h triage</td>
                </tr>
                <tr>
                  <td className="px-4 py-3 font-semibold text-sky-300">Medium</td>
                  <td className="px-4 py-3">
                    Griefing / temporary DoS of vault operations, bounded fee-calculation drift, or API auth weakness
                  </td>
                  <td className="px-4 py-3 font-mono text-emerald-300">$500 – $2,000</td>
                  <td className="px-4 py-3 font-mono">&lt; 24h ack / 3d triage</td>
                </tr>
                <tr>
                  <td className="px-4 py-3 font-semibold text-slate-300">Low</td>
                  <td className="px-4 py-3">
                    Minor contract/API edge cases with no fund loss or availability impact
                  </td>
                  <td className="px-4 py-3 font-mono text-emerald-300">$100 – $500</td>
                  <td className="px-4 py-3 font-mono">&lt; 48h ack / 5d triage</td>
                </tr>
              </tbody>
            </table>
          </div>

          <div className="mt-6 grid gap-4 sm:grid-cols-2">
            <a
              href={REPORT_VULN_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="group rounded-2xl border border-white/10 bg-black/20 p-5 transition-colors hover:border-emerald-300/40"
            >
              <h3 className="text-sm font-semibold text-white">GitHub Private Reporting</h3>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                Preferred. Open a private advisory under the repository&apos;s Security tab.
              </p>
              <span className="mt-3 inline-block text-xs font-medium text-emerald-300 group-hover:text-emerald-200">
                Report a vulnerability ↗
              </span>
            </a>

            <a
              href={`mailto:${SECURITY_EMAIL}?subject=%5BSECURITY%5D%20`}
              className="group rounded-2xl border border-white/10 bg-black/20 p-5 transition-colors hover:border-emerald-300/40"
            >
              <h3 className="text-sm font-semibold text-white">Email</h3>
              <p className="mt-2 text-sm leading-6 text-slate-300">
                Send a report to{" "}
                <span className="font-mono text-emerald-300">{SECURITY_EMAIL}</span> with the
                subject line <span className="font-mono">[SECURITY]</span>.
              </p>
              <span className="mt-3 inline-block text-xs font-medium text-emerald-300 group-hover:text-emerald-200">
                Email the security team ↗
              </span>
            </a>
          </div>

          <ul className="mt-6 space-y-2 text-sm leading-6 text-slate-300">
            <li>
              • <strong>Triage Owner:</strong> Security Council Lead (<span className="font-mono">{SECURITY_EMAIL}</span>).
            </li>
            <li>
              • <strong>Earmarked Bounty Reserve:</strong> $50,000 USDC held in the Nester Treasury Multisig, settled on Stellar Mainnet within 7 days of fix verification.
            </li>
            <li>• Safe harbor: we won&apos;t pursue legal action against good-faith research that respects user funds and privacy.</li>
          </ul>

          <a
            href={SECURITY_POLICY_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-6 inline-flex items-center gap-1.5 text-sm font-medium text-emerald-300 transition-colors hover:text-emerald-200"
          >
            Read the full security policy
            <span aria-hidden="true">↗</span>
          </a>
        </section>
      </Container>
    </main>
  );
}

